# Ship the application container's logs to GCP Cloud Logging via Ops Agent

`docs/spec/v1.md` §8 originally decided against any external log aggregation
("single-user, low volume doesn't justify one"), leaving `docker logs
eve-trader` over SSH as the only way to read production logs. In practice
that's the only way to check on the app, which is enough friction to be worth
fixing — so this reverses that call for the `eve-trader` app container only
(not Caddy or system/SSH logs, which stay out of scope for now).

We chose the **Google Cloud Ops Agent** (installed as an OS package on the VM)
over the Docker `gcplogs` logging driver or a Fluent Bit sidecar. Ops Agent
tails the existing `json-file` logs without changing the container's logging
driver, so `docker logs eve-trader` keeps working with zero extra
configuration — `gcplogs` would replace the driver outright and break that
path unless additionally dual-logged, and a Fluent Bit sidecar adds a second
container and its own config/lifecycle for no benefit at this scale. Ops
Agent is also the only option of the three that extends to Caddy/system logs
later without a mechanism change, should that scope grow.

At the app's actual volume (a few hundred structured JSON lines/day, single
VM), ingestion lands orders of magnitude under Cloud Logging's Always Free
50 GiB/month allotment and well within the free 30-day default retention, so
no exclusion filters or custom retention/bucket config were added.

The VM's service account was given a dedicated `roles/logging.logWriter`-only
binding rather than reusing the default compute service account's broader
default scopes, since the instance runs no other GCP-API-dependent workload.
