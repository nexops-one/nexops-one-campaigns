// SPDX-License-Identifier: Apache-2.0

package adaptertest

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// WriteText writes a human-readable report.
func WriteText(w io.Writer, r Report) error {
	var b strings.Builder
	failures, warnings := 0, 0
	fmt.Fprintf(&b, "conformance: adapter %s\n", r.Adapter)
	for _, s := range r.Suites {
		fmt.Fprintf(&b, "suite %s\n", s.Name)
		for _, c := range s.Results() {
			status := "PASS"
			if len(c.Failures) > 0 {
				status = "FAIL"
			} else if len(c.Warnings) > 0 {
				status = "WARN"
			}
			fmt.Fprintf(&b, "  %s %s\n", status, c.Check)
			for _, f := range append(c.Failures, c.Warnings...) {
				fmt.Fprintf(&b, "      %s\n", f)
			}
			failures += len(c.Failures)
			warnings += len(c.Warnings)
		}
	}
	verdict := "PASS"
	if failures > 0 {
		verdict = "FAIL"
	}
	fmt.Fprintf(&b, "result: %s (%d failure(s), %d warning(s))\n", verdict, failures, warnings)
	_, err := io.WriteString(w, b.String())
	return err
}

type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Name     string       `xml:"name,attr"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

func lines(fs []Finding) string {
	parts := make([]string, len(fs))
	for i, f := range fs {
		parts[i] = f.String()
	}
	return strings.Join(parts, "\n")
}

// WriteJUnit writes the report as JUnit XML: one testsuite per suite and one
// testcase per check. Warnings go to the testcase's system-out.
func WriteJUnit(w io.Writer, r Report) error {
	doc := junitSuites{Name: "adapter conformance: " + r.Adapter}
	for _, s := range r.Suites {
		js := junitSuite{Name: s.Name}
		for _, c := range s.Results() {
			jc := junitCase{Name: c.Check, Classname: "conformance." + strings.ReplaceAll(s.Name, " ", "_")}
			if len(c.Failures) > 0 {
				jc.Failure = &junitFailure{Message: fmt.Sprintf("%d finding(s)", len(c.Failures)), Type: c.Check, Text: lines(c.Failures)}
				js.Failures++
			}
			jc.SystemOut = lines(c.Warnings)
			js.Cases = append(js.Cases, jc)
			js.Tests++
		}
		doc.Tests += js.Tests
		doc.Failures += js.Failures
		doc.Suites = append(doc.Suites, js)
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}
