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

P00 establishes this boundary only. Service implementation begins in its owning engineering/business nodes. No legacy GoJet service source may be imported.
