package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/ui"
	"github.com/naoray/anvil/internal/wtstate"
)

// leaseView is the JSON form of a lease. The token appears only in the
// response that issued it; the stored hash never appears.
type leaseView struct {
	State      string                `json:"state"`
	Owner      string                `json:"owner,omitempty"`
	TaskID     string                `json:"task_id,omitempty"`
	SessionID  string                `json:"session_id,omitempty"`
	AcquiredAt *time.Time            `json:"acquired_at,omitempty"`
	RenewedAt  *time.Time            `json:"renewed_at,omitempty"`
	ExpiresAt  *time.Time            `json:"expires_at,omitempty"`
	TTLSeconds int64                 `json:"ttl_seconds,omitempty"`
	Previous   *wtstate.LeaseHistory `json:"previous,omitempty"`
	Token      string                `json:"token,omitempty"`
}

func newLeaseView(lease *wtstate.Lease, now time.Time, token string) *leaseView {
	view := &leaseView{State: lease.State(now), Token: token}
	if lease == nil {
		return view
	}
	view.Previous = lease.Previous
	if view.State == wtstate.LeaseNone {
		return view
	}
	acquired, renewed, expires := lease.AcquiredAt, lease.RenewedAt, lease.ExpiresAt().UTC()
	view.Owner, view.TaskID, view.SessionID = lease.Owner, lease.TaskID, lease.SessionID
	view.AcquiredAt, view.RenewedAt, view.ExpiresAt, view.TTLSeconds = &acquired, &renewed, &expires, lease.TTLSeconds
	return view
}

// leaseAPIError classifies lease failures with the conflicting lease.
func leaseAPIError(err error) error {
	var leaseErr *wtstate.LeaseError
	if !errors.As(err, &leaseErr) {
		return agentapi.Wrap(agentapi.CodeInternal, err)
	}
	code := agentapi.CodeLeaseHeld
	switch {
	case errors.Is(err, wtstate.ErrLeaseStale):
		code = agentapi.CodeLeaseStale
	case errors.Is(err, wtstate.ErrLeaseNotOwned), errors.Is(err, wtstate.ErrNoLease):
		code = agentapi.CodeLeaseNotOwned
	}
	classified := &agentapi.Error{Code: code, Message: err.Error(), Cause: err}
	if leaseErr.Lease != nil {
		classified.Details = map[string]any{"lease": newLeaseView(leaseErr.Lease, time.Now(), "")}
	}
	return classified
}

func addLeaseFlags(cmd *cobra.Command) {
	cmd.Flags().String("lease-owner", "", "Acquire an advisory lease for this owner (agent name)")
	cmd.Flags().Duration("lease-ttl", wtstate.DefaultLeaseTTL, "Lease duration before it becomes stale without renewal")
	cmd.Flags().String("session-id", "", "External session identifier recorded on the lease (metadata only)")
	cmd.Flags().Bool("takeover-stale", false, "Replace a stale lease (its holder stopped renewing)")
}

func leaseToken(cmd *cobra.Command) (string, error) {
	token := mustGetString(cmd, "token")
	if token == "" {
		token = os.Getenv(LeaseTokenEnv)
	}
	if token == "" {
		return "", agentapi.Errorf(agentapi.CodeInvalidArguments, "lease token required (set %s or --token)", LeaseTokenEnv)
	}
	return token, nil
}

type leaseData struct {
	Worktree worktreeSummary `json:"worktree"`
	Lease    *leaseView      `json:"lease"`
}

// leaseTarget resolves the worktree and its identity for lease commands.
func leaseTarget(args []string, mutate bool) (worktreeSummary, string, string, *lockfile.Lock, error) {
	pc, err := openAgentProject()
	if err != nil {
		return worktreeSummary{}, "", "", nil, err
	}
	wt, err := selectAgentWorktree(pc, args)
	if err != nil {
		return worktreeSummary{}, "", "", nil, err
	}
	id := ""
	var gate *lockfile.Lock
	if mutate {
		gate, err = acquireWorktreeUse(pc, *wt)
		if err != nil {
			return worktreeSummary{}, "", "", nil, err
		}
		identity, _, err := wtstate.EnsureIdentity(wt.Path)
		if err != nil {
			return worktreeSummary{}, "", "", nil, errors.Join(agentapi.Wrap(agentapi.CodeInternal, err), gate.Release())
		}
		id = identity.ID
	}
	summary, _ := summarizeWorktree(*wt)
	return summary, wt.Path, id, gate, nil
}

func leaseSubcommand(use, short, long string, mutating bool, fn func(cmd *cobra.Command, path, id string) (*leaseView, error)) *cobra.Command {
	command := &cobra.Command{
		Use:               use + " [WORKTREE]",
		Short:             short,
		Long:              long,
		Args:              func(cmd *cobra.Command, args []string) error { return invalidArgs(cobra.MaximumNArgs(1)(cmd, args)) },
		ValidArgsFunction: completeWorktreeNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentCommand(cmd, agentCommand{Name: "lease " + use, Mutating: mutating, Human: printLeaseHuman}, func() (result agentResult, runErr error) {
				summary, path, id, gate, err := leaseTarget(args, mutating)
				if err != nil {
					return agentResult{}, err
				}
				defer func() { runErr = errors.Join(runErr, gate.Release()) }()
				view, err := fn(cmd, path, id)
				return agentResult{Data: leaseData{Worktree: summary, Lease: view}}, err
			})
		},
	}
	addJSONFlag(command, agentCommand{Name: "lease " + use, Mutating: mutating, Human: printLeaseHuman})
	return command
}

var leaseCmd = &cobra.Command{
	Use:   "lease",
	Short: "Manage advisory worktree leases for agents",
	Long: `Advisory leases tell other agents and cleanup that a worktree is in use.

A lease records the worktree identity, an owner token (only its SHA-256 is
stored), owner/task/session metadata and acquisition/renewal times. It is
active until renewed_at + ttl, then stale. A task identifier never
authorizes anything: renewing or releasing requires the token, supplied via
ANVIL_LEASE_TOKEN (preferred) or --token. Stale leases are only replaced with
--takeover-stale; 'lease break' forcibly ends an active one and records why.
'anvil remove' refuses to remove a worktree with an active lease unless it
is released or --ignore-lease is given.`,
}

func init() {
	acquire := leaseSubcommand("acquire", "Acquire or renew a lease", "Acquire a lease. The token is returned once; keep it to renew or release.", true,
		func(cmd *cobra.Command, path, id string) (*leaseView, error) {
			owner := mustGetString(cmd, "owner")
			if owner == "" {
				return nil, agentapi.Errorf(agentapi.CodeInvalidArguments, "--owner is required")
			}
			ttl, err := cmd.Flags().GetDuration("ttl")
			if err != nil {
				return nil, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
			}
			existing := os.Getenv(LeaseTokenEnv)
			now := time.Now()
			lease, token, err := wtstate.AcquireLease(path, wtstate.AcquireRequest{
				WorktreeID: id, Token: existing, Owner: owner, TaskID: mustGetString(cmd, "task-id"),
				SessionID: mustGetString(cmd, "session-id"), TTL: ttl, TakeoverStale: mustGetBool(cmd, "takeover-stale"), Now: now,
			})
			if err != nil {
				return nil, leaseAPIError(err)
			}
			issued := ""
			if token != existing {
				issued = token
			}
			return newLeaseView(lease, now, issued), nil
		})
	acquire.Flags().String("owner", "", "Lease owner (agent or person)")
	acquire.Flags().String("task-id", "", "External task identifier (metadata only)")
	acquire.Flags().String("session-id", "", "External session identifier (metadata only)")
	acquire.Flags().Duration("ttl", wtstate.DefaultLeaseTTL, "Time until the lease is stale without renewal")
	acquire.Flags().Bool("takeover-stale", false, "Replace a stale lease")

	renew := leaseSubcommand("renew", "Renew a lease you hold", "", true,
		func(cmd *cobra.Command, path, _ string) (*leaseView, error) {
			token, err := leaseToken(cmd)
			if err != nil {
				return nil, err
			}
			ttl, err := cmd.Flags().GetDuration("ttl")
			if err != nil {
				return nil, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
			}
			now := time.Now()
			lease, err := wtstate.RenewLease(path, token, ttl, now)
			if err != nil {
				return nil, leaseAPIError(err)
			}
			return newLeaseView(lease, now, ""), nil
		})
	renew.Flags().String("token", "", "Lease token (prefer ANVIL_LEASE_TOKEN)")
	renew.Flags().Duration("ttl", wtstate.DefaultLeaseTTL, "New time until the lease is stale")

	release := leaseSubcommand("release", "Release a lease you hold", "", true,
		func(cmd *cobra.Command, path, _ string) (*leaseView, error) {
			token, err := leaseToken(cmd)
			if err != nil {
				return nil, err
			}
			now := time.Now()
			if err := wtstate.ReleaseLease(path, token, now); err != nil {
				return nil, leaseAPIError(err)
			}
			lease, err := wtstate.ReadLease(path)
			if err != nil {
				return nil, agentapi.Wrap(agentapi.CodeInternal, err)
			}
			return newLeaseView(lease, now, ""), nil
		})
	release.Flags().String("token", "", "Lease token (prefer ANVIL_LEASE_TOKEN)")

	breakCmd := leaseSubcommand("break", "Forcibly end another owner's lease", "Explicit operator action: ends the current lease (active or stale) and records the reason.", true,
		func(cmd *cobra.Command, path, _ string) (*leaseView, error) {
			reason := mustGetString(cmd, "reason")
			if reason == "" {
				return nil, agentapi.Errorf(agentapi.CodeInvalidArguments, "--reason is required to break a lease")
			}
			now := time.Now()
			if _, err := wtstate.BreakLease(path, reason, now); err != nil {
				return nil, leaseAPIError(err)
			}
			lease, err := wtstate.ReadLease(path)
			if err != nil {
				return nil, agentapi.Wrap(agentapi.CodeInternal, err)
			}
			return newLeaseView(lease, now, ""), nil
		})
	breakCmd.Flags().String("reason", "", "Why the lease is broken (recorded)")

	show := leaseSubcommand("show", "Show a worktree's lease (read-only)", "", false,
		func(_ *cobra.Command, path, _ string) (*leaseView, error) {
			lease, err := wtstate.ReadLease(path)
			if err != nil {
				return nil, agentapi.Wrap(agentapi.CodeInternal, err)
			}
			return newLeaseView(lease, time.Now(), ""), nil
		})

	leaseCmd.AddCommand(acquire, renew, release, breakCmd, show)
	rootCmd.AddCommand(leaseCmd)
}

func printLeaseHuman(result agentResult) error {
	data, ok := result.Data.(leaseData)
	if !ok || data.Lease == nil {
		return nil
	}
	lease := data.Lease
	if lease.State == wtstate.LeaseNone {
		ui.PrintInfo(fmt.Sprintf("%s: not leased", data.Worktree.Name))
		return nil
	}
	ui.PrintInfo(fmt.Sprintf("%s: %s lease by %q (task %q) until %s", data.Worktree.Name, lease.State, lease.Owner, lease.TaskID, lease.ExpiresAt.Format(time.RFC3339)))
	if lease.Token != "" {
		fmt.Printf("Lease token (shown once; export %s): %s\n", LeaseTokenEnv, lease.Token)
	}
	return nil
}
