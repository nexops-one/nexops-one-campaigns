module github.com/nexops-one/compliance-engine

go 1.24

require (
	github.com/jackc/pgx/v5 v5.7.2
	github.com/nexops-one/compliance-engine/sdk v0.0.0-00010101000000-000000000000
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
	github.com/xuri/excelize/v2 v2.9.1
	golang.org/x/crypto v0.38.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/go-pdf/fpdf v0.9.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/richardlehane/mscfb v1.0.4 // indirect
	github.com/richardlehane/msoleps v1.0.4 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/tiendc/go-deepcopy v1.6.0 // indirect
	github.com/xuri/efp v0.0.1 // indirect
	github.com/xuri/nfp v0.0.1 // indirect
	golang.org/x/net v0.40.0 // indirect
	golang.org/x/sync v0.14.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.25.0 // indirect
)

replace github.com/nexops-one/compliance-engine/sdk => ./sdk
