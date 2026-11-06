# Go Services Boundary

GoJet V10 has eight independently buildable Go executables under the single root module `github.com/Techshrr/GoJet`.

Target command paths are fixed by the Master Plan:

- `services/redirectengine/cmd/server`
- `services/analyticsworker/cmd/worker`
- `services/analyticsreconciler/cmd/reconciler`
- `services/platformapi/cmd/server`
- `services/platformapi/cmd/mailworker`
- `services/platformapi/cmd/fileworker`
- `services/platformapi/cmd/operationsmonitor`
- `services/logreceiver/cmd/server`

These executables are built into the native payload. Their current installation and acceptance status is documented in [P21 status](../docs/p21/STATUS.md). No legacy GoJet service source may be imported.
