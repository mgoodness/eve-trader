# The CLI owns the ESI SSO login flow

The runtime authorization flow — building the PKCE authorize URL, catching the
authorization code on the loopback redirect, exchanging it for tokens, and
persisting the refresh token — originally shipped as
`scripts/esi-sso-wizard.sh`, a bash script with embedded Python that
`recommend` pointed users at. We decided the `eve-trader login` command owns this
flow in Go, and the script is deleted.

The script was the right shape for *provisioning* (registering the ESI
application and proving the redirect, done once in ticket #8), but wrong for the
recurring runtime flow: it duplicates the token logic that already lives in
`internal/esi`, cannot run where there is no browser, and splits the CLI's error
surface across two languages. A user command that authenticates the CLI should
be part of the CLI.

The rejected alternative was shelling out to the script from `login`. That keeps
the gap between the two implementations alive and still needs a shell to be
present, which is not guaranteed.
