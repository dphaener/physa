package propose

import (
	"context"
	"fmt"
	"io"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/learn"
	"github.com/dphaener/physa/internal/mapfile"
)

// Session says how Run drafts.
type Session struct {
	// Agent resolves the agent to draft with. Run calls it only when there
	// are signals to draft from.
	Agent func() (adapter.Adapter, error)
	// Appetite is the most tokens the session may use; zero means
	// draft.DefaultAppetite.
	Appetite int64
	// Progress receives one line before the session starts; nil drops it.
	Progress io.Writer
	// Check is passed to Check.
	Check Options
}

// Run is learning's draft and check stages end to end: it batches the
// signals of res, drafts proposals from them with one agent session and
// checks them on a scratch copy of the map under root. With no signals no
// agent is resolved and the Report's Session is SessionNone. It returns an
// error when the agent cannot be resolved or fails; the circuit breaker
// stopping the session is not one (the Session is SessionStopped, with
// nothing kept). It writes nothing to the map.
func Run(ctx context.Context, root string, m *mapfile.Map, res learn.Result, s Session) (Report, error) {
	appetite := s.Appetite
	if appetite == 0 {
		appetite = draft.DefaultAppetite
	}
	batch := draft.NewBatch(m, res)
	report := Report{
		Version: ReportVersion, Range: res.Range,
		Changes: batch.ChangeCount(), Signals: batch.SignalCount(), Capabilities: batch.CapabilityCount(),
		Session: SessionNone, Appetite: appetite,
		Kept: []Kept{}, Dropped: []Dropped{},
	}
	if batch.Empty() {
		return report, nil
	}
	a, err := s.Agent()
	if err != nil {
		return report, err
	}
	prompt, err := draft.Prompt(m, batch)
	if err != nil {
		return report, err
	}
	sessionID := draft.NewSessionID()
	if s.Progress != nil {
		cuts := ""
		if c := batch.Cuts(); c != "" {
			cuts = "; " + c
		}
		fmt.Fprintf(s.Progress, "physa map learn: appetite %d tokens; batch of %d signals on %d changes, %d capabilities%s; session %s\n",
			appetite, batch.SignalCount(), batch.ChangeCount(), batch.CapabilityCount(), cuts, sessionID)
	}
	out, err := draft.Run(ctx, a, prompt, appetite, sessionID)
	if err != nil {
		return report, err
	}
	report.Tokens, report.DraftSession = out.Tokens, out.SessionID
	if out.Stopped {
		report.Session = SessionStopped
		return report, nil
	}
	report.Session = SessionRan
	proposals, malformed, perr := draft.Parse(out.Text)
	if perr != nil {
		report.Dropped = append(report.Dropped, Dropped{Reason: "the session's output: " + perr.Error()})
		return report, nil
	}
	kept, dropped := Check(root, batch, proposals, s.Check)
	report.Kept = append(report.Kept, kept...)
	report.Dropped = append(report.Dropped, dropped...)
	report.Dropped = append(report.Dropped, FromMalformed(malformed)...)
	return report, nil
}
