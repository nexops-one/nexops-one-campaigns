// SPDX-License-Identifier: Apache-2.0

package deploy_test

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const chart = "helm/compliance-engine"

func needHelm(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is not installed: the chart is not rendered")
	}
}

// render returns the rendered objects by kind, or helm's error output.
func render(t *testing.T, sets ...string) (map[string][]map[string]any, string, error) {
	t.Helper()
	args := []string{"template", "ce", chart}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	var out, errOut bytes.Buffer
	cmd := exec.Command("helm", args...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return nil, errOut.String(), err
	}
	objs := map[string][]map[string]any{}
	dec := yaml.NewDecoder(&out)
	for {
		var o map[string]any
		err := dec.Decode(&o)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("rendered YAML: %v", err)
		}
		if o != nil {
			objs[o["kind"].(string)] = append(objs[o["kind"].(string)], o)
		}
	}
	return objs, out.String(), nil
}

func dig(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[k]
		case int:
			s, _ := v.([]any)
			if k >= len(s) {
				return nil
			}
			v = s[k]
		}
	}
	return v
}

func envOf(container any) map[string]any {
	out := map[string]any{}
	for _, e := range dig(container, "env").([]any) {
		m := e.(map[string]any)
		out[m["name"].(string)] = m
	}
	return out
}

func TestHelmLint(t *testing.T) {
	needHelm(t)
	out, err := exec.Command("helm", "lint", "--strict", chart, "--set", "database.urlSecret.name=pg").CombinedOutput()
	if err != nil {
		t.Fatalf("helm lint: %v\n%s", err, out)
	}
}

func TestHelmSecurityDefaults(t *testing.T) {
	needHelm(t)
	objs, _, err := render(t, "database.urlSecret.name=pg", "tokens.secret.name=tok", "encryption.kekSecret.name=kek", "license.secret.name=lic")
	if err != nil {
		t.Fatal(err)
	}
	dep := objs["Deployment"][0]
	pod := dig(dep, "spec", "template", "spec")
	c := dig(pod, "containers", 0)
	if dig(pod, "securityContext", "runAsNonRoot") != true || dig(pod, "securityContext", "runAsUser") != 65532 ||
		dig(c, "securityContext", "readOnlyRootFilesystem") != true || dig(c, "securityContext", "allowPrivilegeEscalation") != false ||
		dig(c, "securityContext", "capabilities", "drop", 0) != "ALL" || dig(pod, "automountServiceAccountToken") != false {
		t.Fatalf("pod security = %v / %v", dig(pod, "securityContext"), dig(c, "securityContext"))
	}
	env := envOf(c)
	for name, secret := range map[string]string{"COMPLIANCE_DATABASE_URL": "pg", "COMPLIANCE_TOKENS": "tok"} {
		if dig(env[name], "valueFrom", "secretKeyRef", "name") != secret {
			t.Errorf("%s must come from Secret %s: %v", name, secret, env[name])
		}
	}
	if dig(env["COMPLIANCE_ENCRYPTION_KEY_FILE"], "value") != "/etc/compliance/kek/kek" ||
		dig(env["COMPLIANCE_LICENSE_FILE"], "value") != "/etc/compliance/license/license.json" {
		t.Fatalf("file variables = %v %v", env["COMPLIANCE_ENCRYPTION_KEY_FILE"], env["COMPLIANCE_LICENSE_FILE"])
	}
	// The key file is readable by the engine's group only.
	for _, v := range dig(pod, "volumes").([]any) {
		if dig(v, "name") == "kek" && dig(v, "secret", "defaultMode") != 0o440 {
			t.Fatalf("kek volume = %v", v)
		}
	}
	// The chart holds no secret: no Secret object, no inline secret value.
	if len(objs["Secret"]) != 0 {
		t.Fatal("the chart must not create Secrets")
	}
}

func TestHelmMigrationHook(t *testing.T) {
	needHelm(t)
	objs, _, err := render(t, "database.urlSecret.name=pg")
	if err != nil {
		t.Fatal(err)
	}
	job := objs["Job"]
	if len(job) != 1 || dig(job[0], "metadata", "annotations", "helm.sh/hook") != "pre-install,pre-upgrade" ||
		dig(job[0], "spec", "template", "spec", "containers", 0, "args", 1) != "up" {
		t.Fatalf("migration job = %v", job)
	}
	// Its pods are not selected by the Service.
	if dig(job[0], "spec", "template", "metadata", "labels", "app.kubernetes.io/name") == "compliance-engine" {
		t.Fatal("the migration pod must not carry the Deployment's selector labels")
	}
	env := envOf(dig(objs["Deployment"][0], "spec", "template", "spec", "containers", 0))
	if dig(env["COMPLIANCE_AUTO_MIGRATE"], "value") != "false" {
		t.Fatal("with the hook the engine must not migrate")
	}
	objs, _, err = render(t, "database.urlSecret.name=pg", "migrations.enabled=false")
	if err != nil {
		t.Fatal(err)
	}
	env = envOf(dig(objs["Deployment"][0], "spec", "template", "spec", "containers", 0))
	if len(objs["Job"]) != 0 || dig(env["COMPLIANCE_AUTO_MIGRATE"], "value") != "true" {
		t.Fatal("without the hook the engine migrates at start")
	}
}

func TestHelmOptInObjects(t *testing.T) {
	needHelm(t)
	objs, _, err := render(t, "database.urlSecret.name=pg")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"Ingress", "PodDisruptionBudget", "NetworkPolicy"} {
		if len(objs[kind]) != 0 {
			t.Errorf("%s rendered without being enabled", kind)
		}
	}
	objs, _, err = render(t, "database.urlSecret.name=pg", "ingress.enabled=true", "podDisruptionBudget.enabled=true", "networkPolicy.enabled=true")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"Ingress", "PodDisruptionBudget", "NetworkPolicy"} {
		if len(objs[kind]) != 1 {
			t.Errorf("%s not rendered when enabled", kind)
		}
	}
	egress := dig(objs["NetworkPolicy"][0], "spec", "egress").([]any)
	if len(egress) != 2 || dig(egress[1], "ports", 0, "port") != 5432 {
		t.Fatalf("egress = %v", egress)
	}
}

func TestHelmRefusals(t *testing.T) {
	needHelm(t)
	for name, c := range map[string]struct {
		sets []string
		want string
	}{
		"no database":        {nil, "database.urlSecret.name is required"},
		"memory with two":    {[]string{"database.memory=true", "migrations.enabled=false", "replicaCount=2"}, "cannot run with replicaCount > 1"},
		"memory with hook":   {[]string{"database.memory=true"}, "set migrations.enabled=false"},
		"secret in extraEnv": {[]string{"database.urlSecret.name=pg", "extraEnv[0].name=COMPLIANCE_TOKENS", "extraEnv[0].value=x"}, "looks like a secret"},
	} {
		if _, errOut, err := render(t, c.sets...); err == nil || !strings.Contains(errOut, c.want) {
			t.Errorf("%s: err %v, output %s", name, err, errOut)
		}
	}
	// A *_FILE variable is a path, not a secret.
	if _, errOut, err := render(t, "database.urlSecret.name=pg", "extraEnv[0].name=COMPLIANCE_OIDC_CLIENT_SECRET_FILE", "extraEnv[0].value=/run/s"); err != nil {
		t.Fatalf("file path refused: %s", errOut)
	}
}
