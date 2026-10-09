module github.com/nexops-one/compliance-engine/examples/adapter-go

go 1.24

require github.com/nexops-one/compliance-engine/sdk v0.0.0

require (
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	golang.org/x/text v0.14.0 // indirect
)

replace github.com/nexops-one/compliance-engine/sdk => ../../sdk
