# Research: GCP free-tier e2-micro fit for Go+SQLite+Docker workload

**Ticket:** [mgoodness/eve-trader#4](https://github.com/mgoodness/eve-trader/issues/4) (part of the v1 wayfinder map, #1)
**Date:** 2026-09-14
**Scope:** single e2-micro VM running a Go backend + SQLite in a Docker container, serving a low-traffic single-user web frontend, polling ESI for Rens/Heimatar market data every ~5 minutes.

## Recommendation (TL;DR)

**Go: e2-micro + Docker is suitable for this workload**, with a few concrete adjustments to the standing plan:

1. **Deploy in one of the three Always Free regions** — `us-west1` (Oregon), `us-central1` (Iowa), or `us-east1` (South Carolina). This is a hard eligibility constraint, not a preference.
2. **Egress is a non-issue for ESI polling.** The ~15–20 MB Heimatar order-book pulls (per #2's findings) are *inbound* to the VM (ESI → VM), and GCP does not charge or cap inbound data transfer. Only the VM's *outbound* traffic (the tiny outbound request itself, plus whatever the web frontend sends back to the owner's browser) counts against the Always Free 1 GB/month egress allowance, and realistic usage here is tens of MB/month — comfortable headroom.
3. **Docker fits in 1 GB RAM for this specific shape** (one lightweight Go container, SQLite embedded in-process, no heavy sidecars) — but the margin is thin enough that it's worth being deliberate: use a slim/distroless final image and add a small swap file as an OOM safety net. Bare-binary+systemd would buy more headroom but isn't necessary here; see §2 for the reasoning.
4. **The one real recurring cost is the external IPv4 address** (~$3.65/month at current pricing) — it is *not* covered by any Always Free line item. Budget for it; it's unavoidable for a publicly reachable single VM.
5. **HTTPS is required for the EVE SSO callback** and isn't provided out of the box by a bare Compute Engine VM — plan for a domain name + Caddy or nginx+certbot in front of the Go binary (see §4).

Nothing here invalidates the standing plan (e2-micro, Docker, GCP). The only additions are: pick a free-tier region explicitly, add a swap file, and budget for the IP address + a domain name.

## 1. Always Free terms for Compute Engine e2-micro

Source: [cloud.google.com/free/docs/free-cloud-features](https://cloud.google.com/free/docs/free-cloud-features) (Google Cloud Free Program docs, fetched 2026-09-14).

The Compute Engine Always Free entry states, verbatim in substance:

> **Compute Engine**: 1 non-preemptible e2-micro VM instance per month in one of the following US regions: Oregon (`us-west1`), Iowa (`us-central1`), South Carolina (`us-east1`). 30 GB-months standard persistent disk. 1 GB of outbound data transfer from North America to all region destinations (excluding China and Australia) per month.
>
> Your Free Tier e2-micro instance limit is by time, not by instance. Each month, eligible use of all of your e2-micro instances is free until you have used a number of hours equal to the total hours in the current month. Usage calculations are combined across the supported regions.

Key facts pulled out of this:

- **Region restriction is real and specific**: only `us-west1`, `us-central1`, `us-east1` qualify. Any other region (including other US regions) is billed at normal rates.
- **Disk allowance: 30 GB-months of `pd-standard` (standard/HDD-class) persistent disk.** This must be the standard tier, not SSD or Balanced PD — those aren't covered and would incur normal charges. 30 GB is comfortably more than a Go binary + SQLite DB + OS image needs for this workload.
- **Egress allowance: 1 GB/month, "outbound data transfer from North America ... (excluding China and Australia)".** This is the number the ticket asked about directly — see §3 for whether it's actually a constraint.
- **Instance-count nuance**: the free e2-micro-hours are "by time, not by instance" and combined across the three eligible regions — i.e., you get one VM's worth of always-on e2-micro hours per month, not literally "the first VM is free" if you also spin up others. For a single always-on VM this is exactly the steady-state use case the free tier targets.
- GPUs/TPUs are explicitly excluded (not relevant here).
- Billing account note (general Free Program mechanics, not on this specific page but standard GCP behavior): Always Free still requires an active Cloud Billing account with a valid payment method attached; usage beyond any Always Free limit is billed automatically, it doesn't just stop working.

## 2. e2-micro resource envelope and Docker fit

Source: [cloud.google.com/compute/docs/general-purpose-machines](https://cloud.google.com/compute/docs/general-purpose-machines) (E2 shared-core machine types table, fetched 2026-09-14).

Confirmed specs for `e2-micro`:

- **2 vCPUs exposed to the guest OS, but only 0.25 fractional vCPU sustained** — i.e., e2-micro "sustains 2 vCPUs, each for 12.5% of CPU time, totaling 25% CPU time." This is a genuinely small, heavily time-shared slice of a physical core.
- **CPU bursting**: e2-micro can burst up to 100% CPU for short periods — "dozens of seconds," specifically **up to 30 seconds** if fully saturated, via a token-bucket mechanism (sustained heavy use drains the burst budget). This matters for e.g. a burst of JSON-parsing work right after a ~15–20 MB ESI pull lands.
- **Memory: 1 GB.**
- **Max egress bandwidth: up to 1 Gbps** (this is a *rate* cap on the NIC, separate from and much larger than the Always Free *volume* cap of 1 GB/month discussed in §1/§3 — don't confuse the two).

### Does Docker fit in 1 GB RAM?

This part is an engineering judgment call, not a documented GCP spec (Google doesn't publish "Docker on e2-micro" guidance) — flagging that distinction explicitly.

Rough budget for this specific workload (Debian/Ubuntu minimal cloud image + dockerd/containerd + one Go container with embedded SQLite, no separate DB server):

- OS baseline (minimal Debian/Ubuntu cloud image, sshd, systemd, cron/logging): roughly 150–250 MB resident.
- Docker daemon + containerd + shim overhead: roughly 100–250 MB, depending on distro packaging and whether BuildKit/extra features are enabled.
- The Go container itself: a Go binary is a single static process with no separate runtime VM; with `database/sql` + a SQLite driver in-process, working-set memory for a low-traffic single-user app with a modest local DB is typically tens of MB, not hundreds — Go's GC will also return memory to the OS under low load.
- That leaves roughly 500–650 MB of headroom out of 1 GB for OS page cache, SQLite's page cache, and burst activity (e.g., parsing a large ESI response, occasional `apt`/unattended-upgrades runs).

**This fits, but the margin isn't huge**, especially since e2-micro has no swap configured by default — a VM with no swap and a real memory spike (e.g., an unusually large GC pause interacting with an `apt` unattended-upgrade run at the same moment as a large ESI parse) risks the Linux OOM killer, not graceful degradation. Concrete mitigations, all cheap:

- Use a slim/minimal final container image (distroless or Alpine-based) rather than a full Debian image inside the container, to shave tens of MB.
- **Add a 1–2 GB swap file** on the boot disk (well within the 30 GB free persistent-disk allowance). This is the single highest-value mitigation: it turns "OOM-killed process" into "briefly slower," which is exactly the right trade for a background hobby service.
- Avoid running extra always-on sidecar containers (log shippers, monitoring agents, reverse-proxy-as-a-separate-container if avoidable) beyond what's needed — each one adds tens of MB baseline.

**Bare binary + systemd** would recover the ~100–250 MB currently spent on the Docker daemon/containerd, which is meaningful headroom on a 1 GB box — it's the more conservative choice if RAM pressure becomes a real problem in practice. But it isn't necessary to start: this workload (single container, no orchestration needs, low request volume) doesn't need Docker's isolation/portability benefits to justify itself operationally, but it also doesn't obviously exceed the box's budget. **Net call: keep Docker, add swap, watch memory in practice; if `docker stats`/`free -m` show sustained pressure after real-world use, dropping to a systemd-managed static binary is a low-effort fallback**, not a rewrite (the Go binary doesn't change either way).

## 3. Egress cap vs. ESI polling volume — ingress/egress distinction confirmed

Source: [cloud.google.com/vpc/network-pricing](https://cloud.google.com/vpc/network-pricing) (Google Cloud VPC network pricing docs, fetched 2026-09-14).

This directly answers the ticket's core question about whether the free-tier egress cap threatens the ESI polling plan.

**Confirmed: inbound data transfer to a Compute Engine VM is not charged.** The official pricing page states plainly: *"Inbound data transfer: No charge for inbound data transfer."* (It carries a caveat that a few specific inbound-processing services — load balancers, Cloud NAT, protocol forwarding — can incur charges for *processing* inbound data, none of which apply to a plain VM making an outbound HTTP client request.)

The same page also states: *"Responses to requests count as data transfer out and are charged."* — this describes the case where the VM is acting as a *server* responding to a request (e.g., the Go web frontend responding to the owner's browser), which is the actual source of egress here, not ESI polling.

Applying this to the actual traffic shape:

| Traffic | Direction relative to the VM | Counts toward the 1 GB/month free egress? |
|---|---|---|
| eve-trader's outbound HTTP request to ESI (headers only, no body) | Egress (small: ~1–2 KB) | Yes, but negligible |
| ESI's ~15–20 MB order-book response landing on the VM | **Ingress** | **No — not charged, not capped** |
| One-time EVE SSO OAuth redirect/token exchange calls | Mostly small egress (auth code/token exchange bodies are KB-scale) | Yes, but negligible and one-time |
| Go/htmx frontend responses served to the owner's own browser | Egress | Yes, but tiny (single user, small HTML/htmx fragments) |

**Rough math for the realistic month:**

- ESI polling at the ~5-minute cadence ESI itself already enforces via its `expires`/cache headers (per #2's findings — polling faster just returns cached `304`s and wastes nothing new): ~288 polls/day × 30 days = ~8,640 polls/month. At ~1–2 KB of outbound request overhead per poll, that's roughly **10–20 MB/month of actual egress from polling** — the response body itself doesn't count, only the tiny request.
- Frontend usage by a single owner, even generously (several page loads/day with htmx fragment responses): plausibly tens of MB/month, well under 1 GB.
- Combined realistic egress: on the order of **tens of MB/month**, roughly **1–5% of the 1 GB Always Free allowance** — not a binding constraint.
- Even in a worst-case scenario where usage *did* exceed 1 GB, standard-tier internet egress from North America is priced in the range of $0.01–$0.12/GiB depending on destination (per the same pricing page's Internet data transfer table) — i.e., even a multi-GB overage would cost cents to low dollars, not a meaningful expense.

**Bottom line: the sibling ticket's ~15–20 MB-per-pull figure is not a threat to the free-tier egress cap, because that payload arrives as ingress, which GCP does not meter or charge for a plain VM.** The egress cap only bites on the (much smaller) outbound side of this workload.

## 4. Other free-tier gotchas for a small always-on personal web app

- **External IPv4 address is a real, unavoidable recurring cost — and it is not listed under any Always Free benefit.** Per the same VPC network pricing page: *"Static and ephemeral IP addresses in use on standard VM instances: $0.005 / 1 hour."* At 730 hours/month that's **~$3.65/month**, whether the IP is static or ephemeral, as long as it's attached to a running standard VM. This is the one line item that will show up on the bill every month for a publicly reachable single VM. (Reserving a static IP and *not* attaching it to a running resource is billed at a higher rate — so don't reserve one ahead of provisioning the VM.)
- **A firewall rule is required, not automatic.** GCP VPC networks default-deny inbound traffic; you must explicitly create firewall rules to allow inbound `tcp:80`/`tcp:443` (for the web frontend and EVE SSO callback) and ideally restrict `tcp:22` (SSH) to a known source range. This is a one-time `gcloud compute firewall-rules create` (or Terraform) step, not a limit, but it's easy to forget and get a silently-unreachable VM.
- **HTTPS/TLS for the EVE SSO callback needs its own setup — GCP doesn't provide it for a bare Compute Engine VM.** Unlike App Engine, Cloud Run, or a Google-managed load balancer with a Google-managed cert, a plain e2-micro VM has no built-in TLS termination. EVE SSO's documented OAuth2 flow (per `developers.eveonline.com`/`esi-docs`) redirects the browser to the app's registered callback URL after login — a real-world callback endpoint reachable from players' browsers should be HTTPS regardless of whether CCP's registration form strictly enforces the scheme (the current published SSO docs describe the flow without explicitly stating a scheme restriction in the text checked, so treat "does the registration UI accept `http://` for a non-localhost host" as something to verify live at `developers.eveonline.com` when registering the app — plan for HTTPS either way, since sending an OAuth authorization code over plain HTTP is bad practice independent of what CCP's form allows). Practically this means: a domain name pointed at the VM's IP (a few $/year, outside GCP's free tier) plus a lightweight reverse proxy doing automatic cert issuance — Caddy is the simplest option (automatic Let's Encrypt certs with a one-line config), nginx+certbot is the more manual equivalent.
- **Persistent disk must stay on `pd-standard` and ≤30 GB** to remain fully covered — swapping to `pd-balanced`/`pd-ssd`, or growing past 30 GB, moves that disk (or the excess) to normal billing.
- **Sustained-use discounts don't apply to e2 shared-core types** (per the general-purpose machine docs) — irrelevant here since the VM is covered by Always Free anyway, but worth knowing if this ever needs to size up off the free tier.
- **The Always Free e2-micro benefit is account-wide by time, not "your first VM is free" per VM** — running a second VM (e.g., for staging) would not double the free allowance; the hours are pooled.

## Sources

- [Free Google Cloud features and trial offer](https://cloud.google.com/free/docs/free-cloud-features) — Always Free Compute Engine terms, region list, disk/egress allowances. Fetched 2026-09-14.
- [General-purpose machine family for Compute Engine](https://cloud.google.com/compute/docs/general-purpose-machines) — e2-micro vCPU/memory/burst/egress-bandwidth specs. Fetched 2026-09-14.
- [Google Cloud VPC network pricing](https://cloud.google.com/vpc/network-pricing) — inbound-vs-outbound charging rules, external IP address pricing, internet egress pricing table. Fetched 2026-09-14.
- [EVE SSO documentation (esi/esi-docs)](https://github.com/esi/esi-docs/raw/main/docs/services/sso/index.md) — OAuth2 flow description, checked for explicit HTTPS-callback requirement (not explicitly stated in this doc's text; recommend verifying at app registration).
- [mgoodness/eve-trader#2 findings](https://github.com/mgoodness/eve-trader/blob/research/esi-market-data-source/docs/research/esi-market-data-source.md) — source of the ~15–20 MB-per-pull, ~5-minute-cache-TTL figures used in the egress math above.
