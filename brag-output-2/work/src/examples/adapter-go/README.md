# Example adapter: vendor inventory

A complete adapter built only on the public SDK module
(`github.com/nexops-one/compliance-engine/sdk`). It maps a fictional vendor
inventory export (`testdata/vendors.json`) to canonical `ict_provider` and
`cloud_resource` records. The walkthrough is in
[docs/adapters.md](../../docs/adapters.md).

```bash
go test ./...                           # includes the conformance suite (adaptertest.Run)
go run .                                # print the batch
go run . -out ./out                     # write out/manifest.json and out/batches/vendors.json
COMPLIANCE_TOKEN=... go run . -engine http://localhost:8080   # register and push
```

Check the written files with the language-neutral runner, as an adapter in any
other language would:

```bash
compliance-engine adapter test --manifest out/manifest.json --batches out/batches \
  --derived cloud_resource.country
```
