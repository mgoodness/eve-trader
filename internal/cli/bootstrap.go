package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/engine"
	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/spf13/cobra"
)

// newBootstrapCmd builds the `bootstrap` command (spec §7 Bootstrap; ADR
// 0004): it lists the trade station's untracked Hangar stock and seeds a
// held-unlisted lot for each item the pilot confirms on stdin. This is the
// only command that creates seeded lots — `recommend` never does.
func newBootstrapCmd(cfg Config) *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap",
		Short: "Seed pre-existing hangar stock as trading stock",
		RunE: func(cmd *cobra.Command, args []string) error {
			in := cfg.Stdin
			if in == nil {
				in = cmd.InOrStdin()
			}
			return RunBootstrap(cmd.Context(), cfg, in, cmd.OutOrStdout())
		},
	}
}

// RunBootstrap lists the trade station's untracked hangar stock and seeds a
// lot for each item the pilot explicitly confirms on in (spec §7 Bootstrap;
// ADR 0004). It is the only code path that creates a seeded lot, and it
// never seeds anything the pilot did not confirm. Seeded lots are appended
// to the ledger and saved atomically (SaveLedger), so a decline leaves the
// ledger untouched.
func RunBootstrap(ctx context.Context, cfg Config, in io.Reader, out io.Writer) error {
	path, err := ledgerPathFor(cfg)
	if err != nil {
		return err
	}

	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		return fmt.Errorf("opening cache: %w", err)
	}

	facts, err := pilotFacts(ctx, cfg, store)
	if err != nil {
		return fmt.Errorf("reading live pilot facts: %w", err)
	}

	client := esi.NewClient(esi.ClientOptions{
		BaseURL:    cfg.ESIBaseURL,
		UserAgent:  cfg.UserAgent,
		CompatDate: cfg.CompatDate,
	})
	assets, err := cachedCharacterAssets(ctx, store, client, facts.CharacterID, facts.AccessToken)
	if err != nil {
		return err
	}

	lots, err := LoadLedger(path)
	if err != nil {
		return err
	}

	untracked := engine.UntrackedHoldings(lots, assets, cfg.TradeStationID)
	if len(untracked) == 0 {
		fmt.Fprintln(out, "No untracked hangar stock at the trade station.")
		return nil
	}

	confirmed := confirmUntracked(bufio.NewReader(in), untracked, out)
	if len(confirmed) == 0 {
		fmt.Fprintln(out, "Nothing seeded.")
		return nil
	}

	seeded := engine.SeedLots(confirmed, time.Now().UTC())
	if err := SaveLedger(path, append(lots, seeded...)); err != nil {
		return err
	}

	fmt.Fprintln(out, "Seeded:")
	return renderLots(out, seeded)
}

// confirmUntracked prints the untracked holdings and asks the pilot to
// confirm each one on in, returning only the confirmed holdings. A blank,
// EOF, or non-affirmative answer declines; nothing is assumed.
func confirmUntracked(in *bufio.Reader, untracked []engine.UntrackedHolding, out io.Writer) []engine.UntrackedHolding {
	fmt.Fprintln(out, "Untracked hangar stock at the trade station:")
	for _, holding := range untracked {
		fmt.Fprintf(out, "  type %d: %d units\n", holding.TypeID, holding.Quantity)
	}

	var confirmed []engine.UntrackedHolding
	for _, holding := range untracked {
		fmt.Fprintf(out, "Seed type %d (%d units) as trading stock? [y/N] ", holding.TypeID, holding.Quantity)
		line, err := in.ReadString('\n')
		answer := strings.TrimSpace(line)
		if err != nil && answer == "" {
			// EOF with no answer: treat everything still unasked as
			// declined rather than seeding it.
			fmt.Fprintln(out)
			break
		}
		if isAffirmative(answer) {
			confirmed = append(confirmed, holding)
		}
	}
	return confirmed
}

// isAffirmative reports whether a confirmation answer means "yes". Only an
// explicit y/yes confirms; every other answer (including blank) declines.
func isAffirmative(answer string) bool {
	switch strings.ToLower(answer) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
