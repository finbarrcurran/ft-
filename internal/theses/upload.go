package theses

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// scoringLogRepoPath / registryRepoPath are the two doctrine files the uploader
// writes. The registry was split out of the scoring log so the low-churn note
// list no longer inherits the log's per-lock churn.
const (
	scoringLogRepoPath = "theses/_scoring_log.md"
	registryRepoPath   = "theses/_methodology_notes_registry.md"
)

// UploadResult is what the upload endpoint returns to the client.
type UploadResult struct {
	Ticker     string `json:"ticker"`
	Adapter    string `json:"adapter"`
	Version    int    `json:"version"`
	Score      *int   `json:"score,omitempty"`
	MaxScore   int    `json:"maxScore"`
	GitHubPath string `json:"githubPath"`
	GitHubURL  string `json:"githubUrl"`
	CommitSHA  string `json:"commitSha"`
}

// UploadOpts bundles inputs for an upload. ScoringLog and Registry are both
// optional — when present, each replaces its file in the same commit.
type UploadOpts struct {
	ThesisFilename string // original filename from the browser, e.g. "ABBV_v1_locked.md"
	ThesisContent  []byte
	ScoringLog     []byte // optional — empty = don't touch _scoring_log.md
	Registry       []byte // optional — empty = don't touch _methodology_notes_registry.md
}

// Upload writes the thesis (and optionally the scoring log) into the local
// clone, commits with a descriptive message, and pushes to GitHub. Returns
// metadata about what was committed so the frontend can confirm and link.
//
// On any failure the working tree is reset to HEAD so a half-applied commit
// can't poison subsequent uploads.
func (e *Engine) Upload(ctx context.Context, opts UploadOpts) (*UploadResult, error) {
	if !e.Configured() {
		return nil, fmt.Errorf("theses engine not configured — set FT_GITHUB_TOKEN")
	}
	if err := e.EnsureClone(ctx); err != nil {
		return nil, err
	}
	// Pull latest before writing — avoids non-fast-forward pushes.
	if err := e.gitFetchReset(ctx); err != nil {
		return nil, err
	}

	header := ParseHeader(string(opts.ThesisContent))
	if err := header.Validate(); err != nil {
		return nil, fmt.Errorf("invalid thesis MD: %w", err)
	}

	// Build target path inside repo + ensure folder exists.
	targetRel := header.CanonicalPath()
	targetAbs := filepath.Join(e.RepoDir, targetRel)
	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
		return nil, fmt.Errorf("create adapter dir: %w", err)
	}
	if err := os.WriteFile(targetAbs, opts.ThesisContent, 0o644); err != nil {
		return nil, fmt.Errorf("write thesis: %w", err)
	}

	// Optional scoring log replace.
	if len(opts.ScoringLog) > 0 {
		logPath := filepath.Join(e.RepoDir, scoringLogRepoPath)
		if err := os.WriteFile(logPath, opts.ScoringLog, 0o644); err != nil {
			return nil, fmt.Errorf("write scoring log: %w", err)
		}
	}

	// Optional methodology-notes registry replace.
	if len(opts.Registry) > 0 {
		regPath := filepath.Join(e.RepoDir, registryRepoPath)
		if err := os.WriteFile(regPath, opts.Registry, 0o644); err != nil {
			return nil, fmt.Errorf("write methodology registry: %w", err)
		}
	}

	// Stage + commit.
	addArgs := []string{"-C", e.RepoDir, "add", targetRel}
	if len(opts.ScoringLog) > 0 {
		addArgs = append(addArgs, scoringLogRepoPath)
	}
	if len(opts.Registry) > 0 {
		addArgs = append(addArgs, registryRepoPath)
	}
	if out, err := exec.CommandContext(ctx, "git", addArgs...).CombinedOutput(); err != nil {
		_ = e.resetWorktree(ctx)
		return nil, fmt.Errorf("git add: %s: %w", out, err)
	}

	// If add resulted in no staged changes (e.g. byte-identical re-upload),
	// short-circuit with a clear message rather than creating an empty commit.
	diff := exec.CommandContext(ctx, "git", "-C", e.RepoDir, "diff", "--cached", "--quiet")
	if err := diff.Run(); err == nil {
		_ = e.resetWorktree(ctx)
		return nil, fmt.Errorf("no changes — uploaded file is identical to what's already in the repo")
	}

	msg := buildCommitMessage(header)
	commitCmd := exec.CommandContext(ctx, "git", "-C", e.RepoDir,
		"-c", "user.name=FT (Thesis Library)",
		"-c", "user.email=ft-thesis-bot@local",
		"commit", "-m", msg)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		_ = e.resetWorktree(ctx)
		return nil, fmt.Errorf("git commit: %s: %w", out, err)
	}

	// Push with token (Basic auth via extraheader so the URL stays clean).
	bearer := "x-access-token:" + e.GitHubToken
	pushCmd := exec.CommandContext(ctx, "git", "-C", e.RepoDir,
		"-c", "http.https://github.com/.extraheader=Authorization: Basic "+basicAuth(bearer),
		"push", "origin", "main")
	if out, err := pushCmd.CombinedOutput(); err != nil {
		// Roll back local commit so the next upload re-fetches cleanly.
		_ = exec.CommandContext(ctx, "git", "-C", e.RepoDir, "reset", "--hard", "HEAD~1").Run()
		return nil, fmt.Errorf("git push: %s: %w", scrub(string(out), e.GitHubToken), err)
	}

	// Grab the new commit SHA for the response.
	shaOut, _ := exec.CommandContext(ctx, "git", "-C", e.RepoDir, "rev-parse", "HEAD").Output()
	sha := strings.TrimSpace(string(shaOut))

	// Re-sync our index now so the new row is immediately visible.
	if err := e.Sync(ctx); err != nil {
		slog.Warn("theses: post-upload sync failed", "err", err)
	}

	// Best-effort: if a stock_holdings or watchlist row exists for this
	// ticker and has no thesis_link set, fill it in with the canonical URL.
	url := header.CanonicalGitHubURL(e.RepoOwner, e.RepoName)
	if err := e.backfillThesisLink(ctx, header.Ticker, url); err != nil {
		slog.Warn("theses: backfill thesis_link", "err", err)
	}

	return &UploadResult{
		Ticker:     header.Ticker,
		Adapter:    header.Adapter,
		Version:    header.Version,
		Score:      header.Score,
		MaxScore:   header.MaxScore,
		GitHubPath: targetRel,
		GitHubURL:  url,
		CommitSHA:  sha,
	}, nil
}

func (e *Engine) resetWorktree(ctx context.Context) error {
	return exec.CommandContext(ctx, "git", "-C", e.RepoDir, "reset", "--hard", "HEAD").Run()
}

// UploadScoringLogResult is what the scoring-log-only endpoint returns.
type ScoringLogResult struct {
	CommitSHA string `json:"commitSha"`
	GitHubURL string `json:"githubUrl"`
}

// UploadScoringLog replaces theses/_scoring_log.md and/or
// theses/_methodology_notes_registry.md in the local clone, commits both in a
// single commit, and pushes. Used when the user refreshes methodology notes /
// the registry / distribution diagrams without locking a new thesis. At least
// one of log/registry must be non-empty; each empty arg leaves its file alone.
//
// Same locking semantics as Upload(): pulls latest first, resets on
// failure, rolls back local commit on push failure.
func (e *Engine) UploadScoringLog(ctx context.Context, log, registry []byte) (*ScoringLogResult, error) {
	if !e.Configured() {
		return nil, fmt.Errorf("theses engine not configured — set FT_GITHUB_TOKEN")
	}
	if len(log) == 0 && len(registry) == 0 {
		return nil, fmt.Errorf("nothing to upload — provide a scoring log and/or registry")
	}
	if err := e.EnsureClone(ctx); err != nil {
		return nil, err
	}
	if err := e.gitFetchReset(ctx); err != nil {
		return nil, err
	}

	addArgs := []string{"-C", e.RepoDir, "add"}
	if len(log) > 0 {
		if err := os.WriteFile(filepath.Join(e.RepoDir, scoringLogRepoPath), log, 0o644); err != nil {
			return nil, fmt.Errorf("write scoring log: %w", err)
		}
		addArgs = append(addArgs, scoringLogRepoPath)
	}
	if len(registry) > 0 {
		if err := os.WriteFile(filepath.Join(e.RepoDir, registryRepoPath), registry, 0o644); err != nil {
			return nil, fmt.Errorf("write methodology registry: %w", err)
		}
		addArgs = append(addArgs, registryRepoPath)
	}

	if out, err := exec.CommandContext(ctx, "git", addArgs...).CombinedOutput(); err != nil {
		_ = e.resetWorktree(ctx)
		return nil, fmt.Errorf("git add: %s: %w", out, err)
	}
	// Short-circuit if no real change.
	if err := exec.CommandContext(ctx, "git", "-C", e.RepoDir, "diff", "--cached", "--quiet").Run(); err == nil {
		_ = e.resetWorktree(ctx)
		return nil, fmt.Errorf("no changes — uploaded file(s) identical to what's already in the repo")
	}

	var parts []string
	if len(log) > 0 {
		parts = append(parts, "scoring log")
	}
	if len(registry) > 0 {
		parts = append(parts, "methodology registry")
	}
	desc := strings.Join(parts, " + ")
	stamp := time.Now().UTC().Format("2006-01-02")
	commitMsg := fmt.Sprintf("%s%s refresh — uploaded via FT %s", strings.ToUpper(desc[:1]), desc[1:], stamp)
	commitCmd := exec.CommandContext(ctx, "git", "-C", e.RepoDir,
		"-c", "user.name=FT (Thesis Library)",
		"-c", "user.email=ft-thesis-bot@local",
		"commit", "-m", commitMsg)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		_ = e.resetWorktree(ctx)
		return nil, fmt.Errorf("git commit: %s: %w", out, err)
	}

	bearer := "x-access-token:" + e.GitHubToken
	pushCmd := exec.CommandContext(ctx, "git", "-C", e.RepoDir,
		"-c", "http.https://github.com/.extraheader=Authorization: Basic "+basicAuth(bearer),
		"push", "origin", "main")
	if out, err := pushCmd.CombinedOutput(); err != nil {
		_ = exec.CommandContext(ctx, "git", "-C", e.RepoDir, "reset", "--hard", "HEAD~1").Run()
		return nil, fmt.Errorf("git push: %s: %w", scrub(string(out), e.GitHubToken), err)
	}

	shaOut, _ := exec.CommandContext(ctx, "git", "-C", e.RepoDir, "rev-parse", "HEAD").Output()
	sha := strings.TrimSpace(string(shaOut))
	target := scoringLogRepoPath
	if len(log) == 0 {
		target = registryRepoPath
	}
	url := fmt.Sprintf("https://github.com/%s/%s/blob/main/%s",
		e.RepoOwner, e.RepoName, target)

	return &ScoringLogResult{CommitSHA: sha, GitHubURL: url}, nil
}

// backfillThesisLink sets stock_holdings.thesis_link or watchlist.thesis_link
// for the given ticker if (and only if) it's currently NULL/empty. We never
// overwrite a user-set link.
func (e *Engine) backfillThesisLink(ctx context.Context, ticker, url string) error {
	// stocks
	_, _ = e.DB.ExecContext(ctx, `
		UPDATE stock_holdings
		   SET thesis_link = ?
		 WHERE ticker = ?
		   AND deleted_at IS NULL
		   AND (thesis_link IS NULL OR thesis_link = '')`, url, ticker)
	// watchlist
	_, _ = e.DB.ExecContext(ctx, `
		UPDATE watchlist
		   SET thesis_link = ?
		 WHERE ticker = ?
		   AND deleted_at IS NULL
		   AND (thesis_link IS NULL OR thesis_link = '')`, url, ticker)
	return nil
}

func buildCommitMessage(h Header) string {
	verb := "Lock"
	if h.Version > 1 {
		verb = fmt.Sprintf("Re-lock v%d of", h.Version)
	}
	score := ""
	if h.Score != nil {
		score = fmt.Sprintf(" %d/%d", *h.Score, h.MaxScore)
	}
	adapter := strings.ReplaceAll(h.Adapter, "_", "-")
	stamp := time.Now().UTC().Format("2006-01-02")
	return fmt.Sprintf("%s %s thesis (%s%s) — uploaded via FT %s", verb, h.Ticker, adapter, score, stamp)
}
