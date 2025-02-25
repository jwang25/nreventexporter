# nreventexporter
Otel exporter for New Relic Events

## Developing

Whilst there's docs out there this is a quickstart to remind me

- Make a new folder to work in
- Clone this repo into that folder
- Copy the builder-config.yaml file from `tools` to your main folder
- In the main folder, get the `ocb` builder utility, and run `ocb --config builder-config.yaml`
- Initialise and set up go workspace
```
go work init
go work use otelcol-dev
go work use nreventexporter
```
- Edit `otelcol-dev/components.go` and add references to `nreventexporter`
```
➜  otelcol-dev diff components.go components-orig.go 
19d18
<       nreventexporter "github.com/shelson/nreventexporter"
48d46
<               nreventexporter.NewFactory(),
57d54
<       factories.ExporterModules[nreventexporter.NewFactory().Type()] = "github.com/shelson/nreventexporter v0.0.0"
```
- Copy `config.yaml` out of `nreventexporter/tools` and add your NR Api-key
- Run `go run ./otelcol-dev --config ./config.yaml` - this compiles/runs the dev code in place and you can debug using your favourite IDE/tools
- You can use `tools/sendstatsd.py` to send some fake statsd packets for testing

phew!

