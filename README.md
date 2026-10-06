# Operator

Headless staff gateway for a Plat5 deployment. Staff authenticate with your IdP and call the same private service APIs the customer gateway fronts.

The customer gateway fills the subject into the path. This gateway checks a staff JWT, then forwards the path the operator named. That path is the target. Who did it is this gateway's log.

Not part of the plat5 repo. Does not call the customer gateway. Serves no UI; a console is a separate client.

Contract: [`docs/`](docs/). Invariants: [`AGENTS.md`](AGENTS.md).

## License

MIT — see [LICENSE](LICENSE).
