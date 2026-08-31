#!/usr/bin/env python3
from pathlib import Path


def replace_once(path: Path, old: str, new: str, label: str) -> None:
    text = path.read_text(encoding="utf-8")
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected exactly one source boundary, found {count}")
    path.write_text(text.replace(old, new, 1), encoding="utf-8")


httpapi = Path("internal/bio/httpapi.go")
replace_once(
    httpapi,
    """type API struct {\n\tstore           *Store\n\trisk            RiskAuthority\n\ttestAuthEnabled bool\n}\n""",
    """type API struct {\n\tstore           *Store\n\trisk            RiskAuthority\n\ttestAuthEnabled bool\n\tactorResolver   ActorResolver\n}\n""",
    "Bio API struct",
)
replace_once(
    httpapi,
    """func NewAPI(store *Store, risk RiskAuthority, testAuthEnabled bool) (*API, error) {\n\tif store == nil || risk == nil {\n\t\treturn nil, ErrInvalidInput\n\t}\n\treturn &API{store: store, risk: risk, testAuthEnabled: testAuthEnabled}, nil\n}\n""",
    """func NewAPI(store *Store, risk RiskAuthority, testAuthEnabled bool) (*API, error) {\n\tif store == nil || risk == nil {\n\t\treturn nil, ErrInvalidInput\n\t}\n\treturn &API{store: store, risk: risk, testAuthEnabled: testAuthEnabled}, nil\n}\n\nfunc NewAPIWithActorResolver(store *Store, risk RiskAuthority, resolver ActorResolver) (*API, error) {\n\tif store == nil || risk == nil || resolver == nil {\n\t\treturn nil, ErrInvalidInput\n\t}\n\treturn &API{store: store, risk: risk, actorResolver: resolver}, nil\n}\n""",
    "Bio API constructor",
)
replace_once(
    httpapi,
    """func (a *API) authenticate(w http.ResponseWriter, r *http.Request, workspaceID string, mutation bool) (actorContext, bool) {\n\tif !a.testAuthEnabled {\n\t\twriteAPIError(w, http.StatusServiceUnavailable, \"auth_dependency_unavailable\", \"Authentication dependency is not available in this implementation stage.\")\n\t\treturn actorContext{}, false\n\t}\n\tactorID := strings.TrimSpace(r.Header.Get(\"X-GoJet-Test-Actor\"))\n\trole := strings.ToLower(strings.TrimSpace(r.Header.Get(\"X-GoJet-Test-Workspace-Role\")))\n\theaderWorkspace := strings.TrimSpace(r.Header.Get(\"X-GoJet-Test-Workspace\"))\n\tif actorID == \"\" || role == \"\" || headerWorkspace == \"\" || headerWorkspace != workspaceID {\n\t\twriteAPIError(w, http.StatusForbidden, \"forbidden\", \"Workspace access denied.\")\n\t\treturn actorContext{}, false\n\t}\n\tif role != \"owner\" && role != \"admin\" && role != \"member\" && role != \"viewer\" {\n\t\twriteAPIError(w, http.StatusForbidden, \"forbidden\", \"Workspace access denied.\")\n\t\treturn actorContext{}, false\n\t}\n\tif mutation && role == \"viewer\" {\n\t\twriteAPIError(w, http.StatusForbidden, \"read_only\", \"This Workspace role is read-only.\")\n\t\treturn actorContext{}, false\n\t}\n\treturn actorContext{ActorID: actorID, Role: role}, true\n}\n""",
    """func (a *API) authenticate(w http.ResponseWriter, r *http.Request, workspaceID string, mutation bool) (actorContext, bool) {\n\tif a.actorResolver != nil {\n\t\tactor, err := a.actorResolver(r, strings.TrimSpace(workspaceID))\n\t\tif err != nil {\n\t\t\tswitch {\n\t\t\tcase errors.Is(err, ErrAuthenticationRequired):\n\t\t\t\twriteAPIError(w, http.StatusUnauthorized, \"authentication_required\", \"Authentication is required.\")\n\t\t\tcase errors.Is(err, ErrForbidden):\n\t\t\t\twriteAPIError(w, http.StatusForbidden, \"forbidden\", \"Workspace access denied.\")\n\t\t\tdefault:\n\t\t\t\twriteAPIError(w, http.StatusServiceUnavailable, \"auth_dependency_unavailable\", \"Authentication dependency is unavailable.\")\n\t\t\t}\n\t\t\treturn actorContext{}, false\n\t\t}\n\t\tactorID := strings.TrimSpace(actor.ActorID)\n\t\trole := strings.ToLower(strings.TrimSpace(actor.Role))\n\t\tif actorID == \"\" || strings.TrimSpace(workspaceID) == \"\" {\n\t\t\twriteAPIError(w, http.StatusServiceUnavailable, \"auth_dependency_unavailable\", \"Authentication dependency is unavailable.\")\n\t\t\treturn actorContext{}, false\n\t\t}\n\t\tif role != \"owner\" && role != \"admin\" && role != \"member\" && role != \"viewer\" {\n\t\t\twriteAPIError(w, http.StatusForbidden, \"forbidden\", \"Workspace access denied.\")\n\t\t\treturn actorContext{}, false\n\t\t}\n\t\tif mutation && role == \"viewer\" {\n\t\t\twriteAPIError(w, http.StatusForbidden, \"read_only\", \"This Workspace role is read-only.\")\n\t\t\treturn actorContext{}, false\n\t\t}\n\t\treturn actorContext{ActorID: actorID, Role: role}, true\n\t}\n\n\t// Preserve the predecessor P11 test-only adapter for isolated P11 authority tests.\n\tif !a.testAuthEnabled {\n\t\twriteAPIError(w, http.StatusServiceUnavailable, \"auth_dependency_unavailable\", \"Authentication dependency is not available in this implementation stage.\")\n\t\treturn actorContext{}, false\n\t}\n\tactorID := strings.TrimSpace(r.Header.Get(\"X-GoJet-Test-Actor\"))\n\trole := strings.ToLower(strings.TrimSpace(r.Header.Get(\"X-GoJet-Test-Workspace-Role\")))\n\theaderWorkspace := strings.TrimSpace(r.Header.Get(\"X-GoJet-Test-Workspace\"))\n\tif actorID == \"\" || role == \"\" || headerWorkspace == \"\" || headerWorkspace != workspaceID {\n\t\twriteAPIError(w, http.StatusForbidden, \"forbidden\", \"Workspace access denied.\")\n\t\treturn actorContext{}, false\n\t}\n\tif role != \"owner\" && role != \"admin\" && role != \"member\" && role != \"viewer\" {\n\t\twriteAPIError(w, http.StatusForbidden, \"forbidden\", \"Workspace access denied.\")\n\t\treturn actorContext{}, false\n\t}\n\tif mutation && role == \"viewer\" {\n\t\twriteAPIError(w, http.StatusForbidden, \"read_only\", \"This Workspace role is read-only.\")\n\t\treturn actorContext{}, false\n\t}\n\treturn actorContext{ActorID: actorID, Role: role}, true\n}\n""",
    "Bio authenticate",
)

bio_go = Path("services/platformapi/cmd/server/bio.go")
replace_once(
    bio_go,
    """\tstore := bio.NewStore(db, quota)\n\trisk := bio.NewRedisRiskAuthority(redisClient)\n\tapi, err := bio.NewAPI(store, risk, testAuth)\n\tif err != nil {\n\t\treturn nil, false, fmt.Errorf(\"configure Bio API: %w\", err)\n\t}\n""",
    """\tstore := bio.NewStore(db, quota)\n\trisk := bio.NewRedisRiskAuthority(redisClient)\n\tvar api *bio.API\n\tif testAuth {\n\t\tapi, err = bio.NewAPI(store, risk, true)\n\t} else {\n\t\tauthority, authorityErr := buildBioSessionAuthority(db, redisClient)\n\t\tif authorityErr != nil {\n\t\t\treturn nil, false, fmt.Errorf(\"configure Bio authentication authority: %w\", authorityErr)\n\t\t}\n\t\tapi, err = bio.NewAPIWithActorResolver(store, risk, authority.resolve)\n\t}\n\tif err != nil {\n\t\treturn nil, false, fmt.Errorf(\"configure Bio API: %w\", err)\n\t}\n""",
    "platformapi Bio wiring",
)

print("D010 Bio wiring patch applied exactly")
