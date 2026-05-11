package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ecylmz/xvault/internal/auth"
	"github.com/ecylmz/xvault/internal/client"
	"github.com/ecylmz/xvault/internal/config"
	"github.com/ecylmz/xvault/internal/exporter"
	"github.com/ecylmz/xvault/internal/parser"
	"github.com/ecylmz/xvault/internal/queryids"
	"github.com/ecylmz/xvault/internal/store"
	"github.com/ecylmz/xvault/internal/syncer"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

var (
	version = "0.1.0-dev"
	commit  = "unknown"
	date    = "unknown"
)

type state struct {
	json    bool
	quiet   bool
	verbose bool
	config  string
	db      string
	profile string
	authSrc []string
	noColor bool
	cfg     config.Config
	cfgPath string
	started time.Time
}

func Execute(args []string) int {
	st := &state{started: time.Now().UTC()}
	root := &cobra.Command{
		Use:           "xvault",
		Short:         "Personal X/Twitter archive tool",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cfg, cfgPath, err := config.Load(st.config)
			if err != nil {
				return err
			}
			st.cfg, st.cfgPath = cfg, cfgPath
			if st.db != "" {
				st.cfg.Database.Path = st.db
			}
			if len(st.authSrc) > 0 {
				st.cfg.Auth.Sources = st.authSrc
			}
			if st.cfg.Agent.JSONDefault {
				st.json = true
			}
			return nil
		},
	}
	root.PersistentFlags().BoolVar(&st.json, "json", false, "emit a single JSON object")
	root.PersistentFlags().StringVar(&st.config, "config", "", "config file path")
	root.PersistentFlags().StringVar(&st.db, "db", "", "SQLite database path")
	root.PersistentFlags().StringSliceVar(&st.authSrc, "auth-source", nil, "override auth source order (repeat or comma-separate: env,dotenv,firefox,chrome)")
	root.PersistentFlags().StringVar(&st.profile, "profile", "", "profile name")
	root.PersistentFlags().BoolVar(&st.quiet, "quiet", false, "reduce human output")
	root.PersistentFlags().BoolVar(&st.verbose, "verbose", false, "increase diagnostics")
	root.PersistentFlags().BoolVar(&st.noColor, "no-color", false, "disable color")

	addCommands(root, st)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		code := classifyExit(err)
		if st.json {
			writeJSONError(os.Stdout, invokedCommand(args), classifyCode(err), sanitizeErr(err), retryable(err))
		} else {
			_, _ = fmt.Fprintln(os.Stderr, sanitizeErr(err))
		}
		return code
	}
	return 0
}

func addCommands(root *cobra.Command, st *state) {
	root.AddCommand(&cobra.Command{Use: "version", RunE: func(cmd *cobra.Command, args []string) error {
		data := map[string]any{"version": version, "commit": commit, "date": date, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH}
		if st.json {
			writeJSON(os.Stdout, "version", st.started, data)
		} else {
			human(os.Stdout, "xvault %s %s (%s/%s)", version, commit, runtime.GOOS, runtime.GOARCH)
		}
		return nil
	}})
	root.AddCommand(&cobra.Command{Use: "init", RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.EnsureDirs(st.cfg); err != nil {
			return err
		}
		cfgPath := st.cfgPath
		if _, err := os.Stat(cfgPath); errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(cfgPath, []byte(defaultConfigText()), 0o600); err != nil {
				return err
			}
		}
		data := map[string]any{"config_path": cfgPath, "db_path": config.Expand(st.cfg.Database.Path), "dotenv_path": config.Expand(st.cfg.Auth.DotenvPath)}
		if st.json {
			writeJSON(os.Stdout, "init", st.started, data)
		} else {
			human(os.Stdout, "initialized xvault at %s", cfgPath)
		}
		return nil
	}})
	root.AddCommand(statusCmd(st), doctorCmd(st), statsCmd(st), authCmd(st), configCmd(st), syncCmd(st), searchCmd(st), showCmd(st), showURLCmd(st), openCmd(st), threadCmd(st), conversationCmd(st), exportCmd(st), dbCmd(st), backupCmd(st), vacuumCmd(st), serviceCmd(st), refreshIDsCmd(st))
	root.AddCommand(bookmarksCmd(st))
}

func statusCmd(st *state) *cobra.Command {
	return &cobra.Command{Use: "status", RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := config.Expand(st.cfg.Database.Path)
		data := map[string]any{"config_path": st.cfgPath, "db_path": dbPath, "auth": auth.Status(cmd.Context(), st.cfg)}
		if info, err := os.Stat(dbPath); err == nil {
			data["db_exists"], data["db_size_bytes"], data["db_modified_at"] = true, info.Size(), info.ModTime().UTC().Format(time.RFC3339)
		} else {
			data["db_exists"] = false
		}
		if st.json {
			writeJSON(os.Stdout, "status", st.started, data)
		} else {
			human(os.Stdout, "db: %s", dbPath)
		}
		return nil
	}}
}

func doctorCmd(st *state) *cobra.Command {
	return &cobra.Command{Use: "doctor", RunE: func(cmd *cobra.Command, args []string) error {
		checks := []map[string]any{}
		add := func(name string, ok bool, msg string) {
			checks = append(checks, map[string]any{"name": name, "ok": ok, "message": msg})
		}
		add("config_loaded", true, st.cfgPath)
		dbPath := config.Expand(st.cfg.Database.Path)
		if err := config.EnsureDirs(st.cfg); err != nil {
			add("directories", false, err.Error())
		} else {
			add("directories", true, "writable")
		}
		s, err := store.Open(dbPath)
		if err != nil {
			add("database", false, err.Error())
		} else {
			defer s.Close()
			if result, err := s.Integrity(cmd.Context()); err != nil {
				add("database_integrity", false, err.Error())
			} else {
				add("database_integrity", result == "ok", result)
			}
			if runs, err := s.ListSyncRuns(cmd.Context(), "all", "failed", 3); err == nil {
				add("recent_sync_failures", len(runs) == 0, fmt.Sprintf("%d failed run(s)", len(runs)))
			}
		}
		status := auth.Status(cmd.Context(), st.cfg)
		add("auth_cookies", status["auth_token"] == "present" && status["ct0"] == "present", "auth_token="+status["auth_token"]+", ct0="+status["ct0"]+", twid="+status["twid"])
		if info, err := os.Stat(config.Expand(st.cfg.Auth.DotenvPath)); err == nil {
			add("dotenv_permissions", info.Mode().Perm()&0o077 == 0, fmt.Sprintf("mode %03o", info.Mode().Perm()))
		} else {
			add("dotenv_permissions", false, "dotenv not found")
		}
		remoteOK, remoteMsg := gitRemoteStatus(cmd.Context())
		add("git_remote", remoteOK, remoteMsg)
		dockerOK, dockerMsg := dockerDaemonStatus(cmd.Context())
		add("docker_daemon", dockerOK, dockerMsg)
		data := map[string]any{"checks": checks}
		if st.json {
			writeJSON(os.Stdout, "doctor", st.started, data)
		} else {
			for _, c := range checks {
				human(os.Stdout, "%v %s: %s", c["ok"], c["name"], c["message"])
			}
		}
		return nil
	}}
}

func gitRemoteStatus(ctx context.Context) (bool, string) {
	if _, err := exec.LookPath("git"); err != nil {
		return false, "git not found"
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cmdCtx, "git", "remote", "get-url", "origin").Output()
	if err != nil {
		return false, "origin remote not configured"
	}
	remote := strings.TrimSpace(string(out))
	if remote == "" {
		return false, "origin remote not configured"
	}
	return true, remote
}

func dockerDaemonStatus(ctx context.Context) (bool, string) {
	if _, err := exec.LookPath("docker"); err != nil {
		return false, "docker not found"
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(cmdCtx, "docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err == nil {
		return true, "server " + strings.TrimSpace(string(out))
	}
	return false, "docker daemon unavailable"
}

func authCmd(st *state) *cobra.Command {
	cmd := &cobra.Command{Use: "auth"}
	cmd.AddCommand(&cobra.Command{Use: "status", RunE: func(cmd *cobra.Command, args []string) error {
		_, src, err := auth.Resolve(cmd.Context(), st.cfg)
		data := map[string]any{"cookies": auth.Status(cmd.Context(), st.cfg), "source": src.Name}
		if err != nil {
			data["source"] = ""
		}
		if st.json {
			writeJSON(os.Stdout, "auth status", st.started, data)
		} else {
			human(os.Stdout, "auth_token=%s ct0=%s twid=%s", dataCookie(data, "auth_token"), dataCookie(data, "ct0"), dataCookie(data, "twid"))
		}
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error {
		c, src, err := auth.Resolve(cmd.Context(), st.cfg)
		if err != nil {
			return err
		}
		x := client.New(client.Options{Auth: c, MaxRetries: st.cfg.Sync.MaxRetries})
		raw, status, err := x.FetchGraphQL(cmd.Context(), client.Operation{
			Name:    "Viewer",
			QueryID: queryids.Load("").QueryID("Viewer"),
			Variables: map[string]any{
				"withCommunitiesMemberships": true,
				"withSubscribedTab":          true,
				"withCommunitiesCreation":    true,
			},
			FieldToggles: defaultFieldTogglesForApp(),
		})
		if err != nil {
			return err
		}
		var body struct {
			Errors []struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return err
		}
		for _, gqlErr := range body.Errors {
			if gqlErr.Code == 32 || gqlErr.Code == 215 || strings.Contains(strings.ToLower(gqlErr.Message), "not authenticated") {
				return errCode("AUTH_EXPIRED", "authentication cookies were rejected by X")
			}
		}
		data := map[string]any{"source": src.Name, "http_status": status, "authenticated": status >= 200 && status < 300}
		if st.json {
			writeJSON(os.Stdout, "auth test", st.started, data)
		} else {
			human(os.Stdout, "auth test HTTP %d", status)
		}
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "sources", RunE: func(cmd *cobra.Command, args []string) error {
		data := map[string]any{"sources": st.cfg.Auth.Sources, "dotenv_path": config.Expand(st.cfg.Auth.DotenvPath), "browser_sources": auth.BrowserSources()}
		if st.json {
			writeJSON(os.Stdout, "auth sources", st.started, data)
		} else {
			human(os.Stdout, strings.Join(st.cfg.Auth.Sources, ", "))
		}
		return nil
	}})
	var importForce bool
	writeDotenv := func(path string, c auth.Cookies, force bool) error {
		if c.AuthToken == "" || c.CT0 == "" {
			return auth.ErrMissing
		}
		if !force {
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("dotenv file already exists at %s; pass --force to overwrite it", path)
			}
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(auth.DotenvBody(c)), 0o600)
	}
	importEnvCmd := &cobra.Command{Use: "import-env", RunE: func(cmd *cobra.Command, args []string) error {
		c := auth.EnvCookies()
		path := config.Expand(st.cfg.Auth.DotenvPath)
		if err := writeDotenv(path, c, importForce); err != nil {
			return err
		}
		data := map[string]any{"dotenv_path": path, "cookies": auth.RedactedStatus(c)}
		if st.json {
			writeJSON(os.Stdout, "auth import-env", st.started, data)
		} else {
			human(os.Stdout, "imported env cookies to %s", path)
		}
		return nil
	}}
	importEnvCmd.Flags().BoolVar(&importForce, "force", false, "overwrite existing dotenv file")
	cmd.AddCommand(importEnvCmd)
	var importBrowserSource string
	importBrowserCmd := &cobra.Command{Use: "import-browser", RunE: func(cmd *cobra.Command, args []string) error {
		c, src, err := auth.ResolveBrowser(cmd.Context(), importBrowserSource)
		if err != nil {
			return err
		}
		path := config.Expand(st.cfg.Auth.DotenvPath)
		if err := writeDotenv(path, c, importForce); err != nil {
			return err
		}
		data := map[string]any{"dotenv_path": path, "source": src.Name, "cookies": auth.RedactedStatus(c)}
		if st.json {
			writeJSON(os.Stdout, "auth import-browser", st.started, data)
		} else {
			human(os.Stdout, "imported %s cookies to %s", src.Name, path)
		}
		return nil
	}}
	importBrowserCmd.Flags().StringVar(&importBrowserSource, "source", "firefox", "browser source: firefox or chrome")
	importBrowserCmd.Flags().BoolVar(&importForce, "force", false, "overwrite existing dotenv file")
	cmd.AddCommand(importBrowserCmd)
	return cmd
}

func configCmd(st *state) *cobra.Command {
	cmd := &cobra.Command{Use: "config"}
	cmd.AddCommand(&cobra.Command{Use: "show", RunE: func(cmd *cobra.Command, args []string) error {
		safe := st.cfg
		safe.Auth.AuthToken, safe.Auth.CT0, safe.Auth.TWID = "", "", ""
		if st.json {
			writeJSON(os.Stdout, "config show", st.started, safe)
		} else {
			human(os.Stdout, "config: %s", st.cfgPath)
		}
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "get KEY", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		data := map[string]any{"key": args[0], "value": getConfigValue(st.cfg, args[0])}
		if st.json {
			writeJSON(os.Stdout, "config get", st.started, data)
		} else {
			human(os.Stdout, "%v", data["value"])
		}
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "set KEY VALUE", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if err := setConfigValue(&st.cfg, args[0], args[1]); err != nil {
			return err
		}
		out, err := toml.Marshal(st.cfg)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(st.cfgPath), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(st.cfgPath, out, 0o600); err != nil {
			return err
		}
		data := map[string]any{"key": args[0], "value": getConfigValue(st.cfg, args[0]), "config_path": st.cfgPath}
		if st.json {
			writeJSON(os.Stdout, "config set", st.started, data)
		} else {
			human(os.Stdout, "updated %s", args[0])
		}
		return nil
	}})
	return cmd
}

func syncCmd(st *state) *cobra.Command {
	var count, maxPages int
	var all, full, withThreads bool
	var folder, threadMode string
	var threadLimit int
	cmd := &cobra.Command{Use: "sync [collection]", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		collections := []string{"likes", "bookmarks", "tweets", "reposts", "replies"}
		if len(args) == 1 {
			collections = []string{args[0]}
		}
		cookies, _, err := auth.Resolve(cmd.Context(), st.cfg)
		if err != nil {
			return err
		}
		dbPath := config.Expand(st.cfg.Database.Path)
		s, err := store.Open(dbPath)
		if err != nil {
			return err
		}
		defer s.Close()
		x := client.New(client.Options{Auth: cookies, MaxRetries: st.cfg.Sync.MaxRetries})
		sy := syncer.New(x, s, queryids.Load(""), dbPath, cookies.TWID, time.Duration(st.cfg.Sync.RequestDelayMS)*time.Millisecond)
		results := []syncer.Result{}
		threadsExpanded := 0
		countChanged := cmd.Flags().Changed("count")
		for _, col := range collections {
			reqCount, reqAll := syncCountForCollection(st.cfg, col, count, all, countChanged)
			res, err := sy.Sync(cmd.Context(), syncer.Request{Collection: col, Count: reqCount, MaxPages: maxPages, All: reqAll, Full: full, Folder: folder})
			results = append(results, res)
			if err != nil {
				return err
			}
			if withThreads {
				ids, err := s.CollectionTweetIDs(cmd.Context(), col, threadLimit)
				if err != nil {
					return err
				}
				for _, id := range ids {
					if err := expandTweetDetail(cmd.Context(), st, s, id, threadMode); err != nil {
						return err
					}
					threadsExpanded++
				}
			}
		}
		data := map[string]any{"results": results, "with_threads": withThreads, "threads_expanded": threadsExpanded, "thread_mode": threadMode, "thread_limit": threadLimit}
		if len(results) == 1 {
			data = map[string]any{"result": results[0], "with_threads": withThreads, "threads_expanded": threadsExpanded, "thread_mode": threadMode, "thread_limit": threadLimit}
		}
		if st.json {
			writeJSON(os.Stdout, "sync", st.started, data)
		} else {
			human(os.Stdout, "synced %d collection(s)", len(results))
		}
		return nil
	}}
	cmd.PersistentFlags().IntVar(&count, "count", 100, "maximum tweets/items to process")
	cmd.PersistentFlags().IntVar(&maxPages, "max-pages", 0, "maximum GraphQL pages to fetch")
	cmd.PersistentFlags().BoolVar(&all, "all", false, "remove item limit")
	cmd.PersistentFlags().BoolVar(&full, "full", false, "ignore incremental boundaries")
	cmd.PersistentFlags().StringVar(&folder, "folder", "", "bookmark folder name")
	cmd.PersistentFlags().BoolVar(&withThreads, "with-threads", false, "expand threads after sync")
	cmd.PersistentFlags().StringVar(&threadMode, "thread-mode", "thread", "thread expansion mode")
	cmd.PersistentFlags().IntVar(&threadLimit, "thread-limit", 200, "thread expansion limit")
	var runsCollection, runsStatus string
	var runsLimit int
	runsCmd := &cobra.Command{Use: "runs", RunE: func(c *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		runs, err := s.ListSyncRuns(c.Context(), runsCollection, runsStatus, runsLimit)
		if err != nil {
			return err
		}
		data := map[string]any{"runs": runs, "count": len(runs), "collection": runsCollection, "status": runsStatus}
		if st.json {
			writeJSON(os.Stdout, "sync runs", st.started, data)
		} else {
			for _, run := range runs {
				human(os.Stdout, "%s %s %s pages=%d tweets=%d", run.ID, run.CollectionType, run.Status, run.PagesFetched, run.TweetsSeen)
			}
		}
		return nil
	}}
	runsCmd.Flags().StringVar(&runsCollection, "collection", "all", "collection filter")
	runsCmd.Flags().StringVar(&runsStatus, "status", "all", "status filter")
	runsCmd.Flags().IntVar(&runsLimit, "limit", 20, "run limit")
	cmd.AddCommand(runsCmd)
	cmd.AddCommand(&cobra.Command{Use: "checkpoints", RunE: func(c *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		checkpoints, err := s.ListCheckpoints(c.Context())
		if err != nil {
			return err
		}
		data := map[string]any{"checkpoints": checkpoints, "count": len(checkpoints)}
		if st.json {
			writeJSON(os.Stdout, "sync checkpoints", st.started, data)
		} else {
			for _, cp := range checkpoints {
				human(os.Stdout, "%s %s seen=%d cursor=%s", cp.CollectionType, cp.Status, cp.TotalSeen, cp.Cursor)
			}
		}
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "sanitize-runs", RunE: func(c *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		updated, err := s.SanitizeSyncRunErrors(c.Context())
		if err != nil {
			return err
		}
		data := map[string]any{"updated": updated}
		if st.json {
			writeJSON(os.Stdout, "sync sanitize-runs", st.started, data)
		} else {
			human(os.Stdout, "sanitized %d sync run(s)", updated)
		}
		return nil
	}})
	for _, name := range []string{"likes", "bookmarks", "tweets", "reposts", "replies", "posts", "feed"} {
		n := name
		cmd.AddCommand(&cobra.Command{Use: n, RunE: func(c *cobra.Command, args []string) error {
			cmd.SetArgs(append([]string{n}, args...))
			return cmd.RunE(c, []string{n})
		}})
	}
	return cmd
}

func syncCountForCollection(cfg config.Config, collection string, flagCount int, flagAll, countChanged bool) (int, bool) {
	if flagAll || countChanged {
		return flagCount, flagAll
	}
	count := cfg.Sync.DefaultCount
	switch collection {
	case "likes":
		count = cfg.Sync.DefaultLikeCount
	case "bookmarks":
		count = cfg.Sync.DefaultBookmarkCount
	}
	if count < 0 {
		return 0, true
	}
	return count, false
}

func searchCmd(st *state) *cobra.Command {
	var source, author, from, to, folder string
	var limit, offset int
	var hasMedia, hasLink, recent bool
	cmd := &cobra.Command{Use: "search [QUERY]", Args: func(cmd *cobra.Command, args []string) error {
		if recent {
			return cobra.MaximumNArgs(1)(cmd, args)
		}
		return cobra.ExactArgs(1)(cmd, args)
	}, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		query := ""
		if len(args) > 0 {
			query = args[0]
		}
		results, err := s.SearchWithFilters(cmd.Context(), query, source, author, folder, from, to, hasMedia, hasLink, limit, offset)
		if err != nil {
			return err
		}
		data := map[string]any{"query": query, "source": source, "recent": recent, "limit": limit, "offset": offset, "total_estimate": len(results), "results": results}
		if st.json {
			writeJSON(os.Stdout, "search", st.started, data)
		} else {
			for _, r := range results {
				human(os.Stdout, "@%s %s\n%s\n%s\n", r.AuthorUsername, r.CreatedAt, r.TextPreview, r.URL)
			}
		}
		return nil
	}}
	cmd.Flags().StringVar(&source, "source", "all", "source collection")
	cmd.Flags().IntVar(&limit, "limit", 10, "result limit")
	cmd.Flags().IntVar(&offset, "offset", 0, "result offset")
	cmd.Flags().StringVar(&author, "author", "", "author username")
	cmd.Flags().StringVar(&from, "from", "", "from date")
	cmd.Flags().StringVar(&to, "to", "", "to date")
	cmd.Flags().BoolVar(&hasMedia, "has-media", false, "filter media")
	cmd.Flags().BoolVar(&hasLink, "has-link", false, "filter links")
	cmd.Flags().BoolVar(&recent, "recent", false, "list recent records without requiring a query")
	cmd.Flags().StringVar(&folder, "folder", "", "bookmark folder")
	return cmd
}

func bookmarksCmd(st *state) *cobra.Command {
	cmd := &cobra.Command{Use: "bookmarks"}
	cmd.AddCommand(&cobra.Command{Use: "folders", RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		folders, err := s.BookmarkFolders(cmd.Context())
		if err != nil {
			return err
		}
		data := map[string]any{"folders": folders, "count": len(folders)}
		if st.json {
			writeJSON(os.Stdout, "bookmarks folders", st.started, data)
		} else {
			for _, f := range folders {
				human(os.Stdout, "%s %d", f.Name, f.Count)
			}
		}
		return nil
	}})
	return cmd
}

func showCmd(st *state) *cobra.Command {
	var includeRaw bool
	cmd := &cobra.Command{Use: "show TWEET_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if includeRaw && st.cfg.Agent.SafeMode && !st.cfg.Agent.AllowRawOutput {
			return errCode("RAW_OUTPUT_BLOCKED", "raw output is blocked by agent safe mode")
		}
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		data, err := s.Show(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if includeRaw {
			if rawID, ok := data["raw_json_id"].(string); ok && rawID != "" {
				raw, err := s.RawPayload(cmd.Context(), rawID)
				if err != nil {
					return err
				}
				data["raw_json"] = raw
			}
		}
		if st.json {
			writeJSON(os.Stdout, "show", st.started, data)
		} else {
			human(os.Stdout, "%s\n%s", data["text"], data["url"])
		}
		return nil
	}}
	cmd.Flags().BoolVar(&includeRaw, "include-raw", false, "include raw JSON when safe mode allows it")
	return cmd
}

func showURLCmd(st *state) *cobra.Command {
	return &cobra.Command{Use: "show-url URL", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		data, err := s.ShowByURL(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if st.json {
			writeJSON(os.Stdout, "show-url", st.started, data)
		} else {
			human(os.Stdout, "%s\n%s", data["text"], data["url"])
		}
		return nil
	}}
}

func openCmd(st *state) *cobra.Command {
	return &cobra.Command{Use: "open TWEET_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		data, err := s.Show(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		url, _ := data["url"].(string)
		if runtime.GOOS == "darwin" {
			if err := exec.CommandContext(cmd.Context(), "open", url).Run(); err != nil {
				return err
			}
		} else {
			if err := exec.CommandContext(cmd.Context(), "xdg-open", url).Run(); err != nil {
				return err
			}
		}
		if st.json {
			writeJSON(os.Stdout, "open", st.started, map[string]any{"url": url})
		}
		return nil
	}}
}

func threadCmd(st *state) *cobra.Command {
	var mode string
	var limit int
	cmd := &cobra.Command{Use: "thread TWEET_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		if err := expandTweetDetail(cmd.Context(), st, s, args[0], mode); err != nil {
			return err
		}
		data, err := s.Thread(cmd.Context(), args[0], mode, limit)
		if err != nil {
			return err
		}
		if st.json {
			writeJSON(os.Stdout, "thread", st.started, data)
		} else {
			human(os.Stdout, "%s %v tweets", data["thread_id"], data["tweet_count"])
		}
		return nil
	}}
	cmd.Flags().StringVar(&mode, "mode", "thread", "thread or conversation")
	cmd.Flags().IntVar(&limit, "limit", 200, "maximum tweets")
	return cmd
}

func conversationCmd(st *state) *cobra.Command {
	var limit int
	cmd := &cobra.Command{Use: "conversation TWEET_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		if err := expandTweetDetail(cmd.Context(), st, s, args[0], "conversation"); err != nil {
			return err
		}
		data, err := s.Thread(cmd.Context(), args[0], "conversation", limit)
		if err != nil {
			return err
		}
		if st.json {
			writeJSON(os.Stdout, "conversation", st.started, data)
		} else {
			human(os.Stdout, "%s %v tweets", data["thread_id"], data["tweet_count"])
		}
		return nil
	}}
	cmd.Flags().IntVar(&limit, "limit", 500, "maximum tweets")
	return cmd
}

func expandTweetDetail(ctx context.Context, st *state, s *store.Store, tweetID, collection string) error {
	cookies, _, err := auth.Resolve(ctx, st.cfg)
	if err != nil {
		return err
	}
	qids := queryids.Load("")
	x := client.New(client.Options{Auth: cookies, MaxRetries: st.cfg.Sync.MaxRetries})
	raw, _, err := x.FetchGraphQL(ctx, client.Operation{
		Name:    "TweetDetail",
		QueryID: qids.QueryID("TweetDetail"),
		Variables: map[string]any{
			"focalTweetId": tweetID, "includePromotedContent": true,
			"withCommunity": false, "withQuickPromoteEligibilityTweetFields": true,
			"withBirdwatchNotes": false, "withVoice": false,
		},
		FieldToggles: defaultFieldTogglesForApp(),
	})
	if err != nil {
		return err
	}
	rawID, err := s.SaveRaw(ctx, "graphql", "TweetDetail", raw)
	if err != nil {
		return err
	}
	page, err := parser.Timeline(raw, collection, "", "", rawID)
	if err != nil {
		return err
	}
	return s.UpsertPage(ctx, page)
}

func defaultFieldTogglesForApp() map[string]any {
	return map[string]any{
		"withPayments":                false,
		"withAuxiliaryUserLabels":     false,
		"withArticleRichContentState": false,
		"withArticlePlainText":        false,
		"withArticleSummaryText":      false,
		"withArticleVoiceOver":        false,
		"withGrokAnalyze":             false,
		"withDisallowedReplyControls": false,
	}
}

func exportCmd(st *state) *cobra.Command {
	cmd := &cobra.Command{Use: "export"}
	addExport := func(name string, run func(context.Context, *store.Store, string, string, string) (map[string]any, error)) {
		var collection, folder, output string
		c := &cobra.Command{Use: name, RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open(config.Expand(st.cfg.Database.Path))
			if err != nil {
				return err
			}
			defer s.Close()
			data, err := run(cmd.Context(), s, collection, folder, output)
			if err != nil {
				return err
			}
			if st.json {
				writeJSON(os.Stdout, "export "+name, st.started, data)
			} else {
				human(os.Stdout, "exported %s", data["output"])
			}
			return nil
		}}
		c.Flags().StringVar(&collection, "collection", "all", "collection")
		c.Flags().StringVar(&folder, "folder", "", "bookmark folder")
		c.Flags().StringVar(&output, "output", "", "output path")
		cmd.AddCommand(c)
	}
	var pretty bool
	jsonCmd := &cobra.Command{Use: "json", RunE: func(cmd *cobra.Command, args []string) error {
		collection, _ := cmd.Flags().GetString("collection")
		folder, _ := cmd.Flags().GetString("folder")
		output, _ := cmd.Flags().GetString("output")
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		data, err := exporter.JSONWithFolder(cmd.Context(), s, collection, folder, output, pretty)
		if err != nil {
			return err
		}
		if st.json {
			writeJSON(os.Stdout, "export json", st.started, data)
		} else {
			human(os.Stdout, "exported %s", output)
		}
		return nil
	}}
	jsonCmd.Flags().String("collection", "all", "collection")
	jsonCmd.Flags().String("folder", "", "bookmark folder")
	jsonCmd.Flags().String("output", "", "output path")
	jsonCmd.Flags().BoolVar(&pretty, "pretty", false, "pretty JSON")
	cmd.AddCommand(jsonCmd)
	addExport("csv", exporter.CSVWithFolder)
	addExport("html", exporter.HTMLWithFolder)
	addExport("markdown", func(ctx context.Context, s *store.Store, c, f, o string) (map[string]any, error) {
		return exporter.MarkdownWithFolder(ctx, s, c, f, o, false)
	})
	addExport("hermes", func(ctx context.Context, s *store.Store, c, f, o string) (map[string]any, error) {
		if c == "" {
			c = "all"
		}
		return exporter.MarkdownWithFolder(ctx, s, c, f, o, true)
	})
	addExport("obsidian", func(ctx context.Context, s *store.Store, c, f, o string) (map[string]any, error) {
		return exporter.MarkdownWithFolder(ctx, s, c, f, o, false)
	})
	return cmd
}

func dbCmd(st *state) *cobra.Command {
	cmd := &cobra.Command{Use: "db"}
	cmd.AddCommand(&cobra.Command{Use: "migrate", RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		data := map[string]any{"db_path": config.Expand(st.cfg.Database.Path), "migrated": true}
		if st.json {
			writeJSON(os.Stdout, "db migrate", st.started, data)
		} else {
			human(os.Stdout, "migrated")
		}
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "integrity", RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		result, err := s.Integrity(cmd.Context())
		if err != nil {
			return err
		}
		data := map[string]any{"result": result}
		if st.json {
			writeJSON(os.Stdout, "db integrity", st.started, data)
		} else {
			human(os.Stdout, result)
		}
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "rebuild-fts", RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		if err := s.RebuildFTS(cmd.Context()); err != nil {
			return err
		}
		data := map[string]any{"rebuilt": true}
		if st.json {
			writeJSON(os.Stdout, "db rebuild-fts", st.started, data)
		} else {
			human(os.Stdout, "rebuilt FTS")
		}
		return nil
	}})
	return cmd
}

func vacuumCmd(st *state) *cobra.Command {
	return &cobra.Command{Use: "vacuum", RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		if err := s.Vacuum(cmd.Context()); err != nil {
			return err
		}
		if st.json {
			writeJSON(os.Stdout, "vacuum", st.started, map[string]any{"vacuumed": true})
		} else {
			human(os.Stdout, "vacuumed")
		}
		return nil
	}}
}

func statsCmd(st *state) *cobra.Command {
	return &cobra.Command{Use: "stats", RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		data, err := s.Stats(cmd.Context())
		if err != nil {
			return err
		}
		if st.json {
			writeJSON(os.Stdout, "stats", st.started, data)
		} else {
			human(os.Stdout, "tweets: %v", data["total_unique_tweets"])
		}
		return nil
	}}
}

func backupCmd(st *state) *cobra.Command {
	cmd := &cobra.Command{Use: "backup"}
	var output string
	cmd.AddCommand(&cobra.Command{Use: "create", RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer s.Close()
		data, err := exporter.Backup(cmd.Context(), s, output)
		if err != nil {
			return err
		}
		if st.json {
			writeJSON(os.Stdout, "backup create", st.started, data)
		} else {
			human(os.Stdout, "backup: %s", data["output"])
		}
		return nil
	}})
	cmd.PersistentFlags().StringVar(&output, "output", "", "backup path")
	cmd.AddCommand(&cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
		files, _ := filepath.Glob(filepath.Join(os.Getenv("HOME"), ".local/state/xvault/backups/*.sqlite"))
		if st.json {
			writeJSON(os.Stdout, "backup list", st.started, map[string]any{"backups": files})
		} else {
			human(os.Stdout, strings.Join(files, "\n"))
		}
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "verify PATH", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		db, err := sql.Open("sqlite", args[0])
		if err != nil {
			return err
		}
		defer db.Close()
		var result string
		err = db.QueryRowContext(cmd.Context(), "PRAGMA integrity_check").Scan(&result)
		if err != nil {
			return err
		}
		if st.json {
			writeJSON(os.Stdout, "backup verify", st.started, map[string]any{"path": args[0], "result": result})
		} else {
			human(os.Stdout, result)
		}
		return nil
	}})
	return cmd
}

func serviceCmd(st *state) *cobra.Command {
	cmd := &cobra.Command{Use: "service"}
	sys := &cobra.Command{Use: "systemd"}
	var user bool
	sys.AddCommand(&cobra.Command{Use: "print", Run: func(cmd *cobra.Command, args []string) {
		_ = user
		fmt.Print("[Unit]\nDescription=xvault bookmark sync\n\n[Service]\nType=oneshot\nExecStart=/usr/local/bin/xvault sync bookmarks --count 300 --json\n\n[Unit]\nDescription=Run xvault bookmark sync every 3 hours\n\n[Timer]\nOnBootSec=10min\nOnUnitActiveSec=3h\nPersistent=true\n\n[Install]\nWantedBy=timers.target\n")
	}})
	sys.PersistentFlags().BoolVar(&user, "user", false, "print user service example")
	cmd.AddCommand(sys)
	cron := &cobra.Command{Use: "cron"}
	cron.AddCommand(&cobra.Command{Use: "print", Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("0 */3 * * * /usr/local/bin/xvault sync bookmarks --count 300 --json >> ~/.local/state/xvault/logs/bookmarks.log 2>&1")
	}})
	cmd.AddCommand(cron)
	return cmd
}

func refreshIDsCmd(st *state) *cobra.Command {
	return &cobra.Command{Use: "refresh-ids", RunE: func(cmd *cobra.Command, args []string) error {
		cache, err := queryids.Refresh("")
		if err != nil {
			return err
		}
		data := map[string]any{"operations": cache.Operations, "refreshed": true}
		if st.json {
			writeJSON(os.Stdout, "refresh-ids", st.started, data)
		} else {
			human(os.Stdout, "query IDs loaded: %d", len(cache.Operations))
		}
		return nil
	}}
}

func classifyExit(err error) int {
	if errors.Is(err, auth.ErrMissing) {
		return 4
	}
	if strings.Contains(err.Error(), "HTTP 401") || strings.Contains(err.Error(), "Could not authenticate") {
		return 4
	}
	if coded, ok := err.(codedError); ok && coded.code == "AUTH_EXPIRED" {
		return 4
	}
	if strings.Contains(err.Error(), "HTTP 404") || strings.Contains(err.Error(), "Query not found") {
		return 7
	}
	if coded, ok := err.(codedError); ok && coded.code == "RAW_OUTPUT_BLOCKED" {
		return 1
	}
	return 1
}

func classifyCode(err error) string {
	if errors.Is(err, auth.ErrMissing) {
		return "AUTH_MISSING"
	}
	if strings.Contains(err.Error(), "HTTP 401") || strings.Contains(err.Error(), "Could not authenticate") {
		return "AUTH_EXPIRED"
	}
	if coded, ok := err.(codedError); ok && coded.code != "" {
		return coded.code
	}
	if strings.Contains(err.Error(), "HTTP 404") || strings.Contains(err.Error(), "Query not found") {
		return "QUERY_ID_STALE"
	}
	if strings.Contains(err.Error(), "rate limited") {
		return "RATE_LIMITED"
	}
	return "ERROR"
}

func retryable(err error) bool { return strings.Contains(err.Error(), "rate limited") }

func sanitizeErr(err error) string {
	msg := err.Error()
	for _, token := range []string{"auth_token", "ct0", "Authorization", "Cookie", "x-csrf-token"} {
		msg = strings.ReplaceAll(msg, token, "[REDACTED]")
	}
	return msg
}

func invokedCommand(args []string) string {
	parts := []string{}
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if !strings.Contains(arg, "=") && arg != "--json" && arg != "--quiet" && arg != "--verbose" && arg != "--no-color" && arg != "--all" && arg != "--full" && arg != "--with-threads" && arg != "--has-media" && arg != "--has-link" && arg != "--pretty" {
				skipNext = true
			}
			continue
		}
		parts = append(parts, arg)
	}
	if len(parts) == 0 {
		return "xvault"
	}
	return strings.Join(parts, " ")
}

type codedError struct{ code, message string }

func (e codedError) Error() string       { return e.message }
func errCode(code, message string) error { return codedError{code: code, message: message} }

func defaultConfigText() string {
	return `[auth]
sources = ["env", "dotenv", "config", "firefox", "chrome", "macos_keychain"]
dotenv_path = "~/.config/xvault/.env"

[sync]
default_count = 100
default_like_count = -1
default_bookmark_count = -1
request_delay_ms = 750
max_retries = 5
stop_after_consecutive_rate_limits = 3
store_raw = true
checkpoint_every_items = 25
checkpoint_every_pages = 1
default_thread_limit = 200
default_conversation_limit = 500
feed_default_hours = 24

[database]
path = "~/.local/share/xvault/xvault.sqlite"
wal = true
busy_timeout_ms = 5000
foreign_keys = true

[export]
dir = "~/.local/share/xvault/exports"
markdown_layout = "collection/year"
overwrite = false
html_warn_size_mb = 40

[agent]
safe_mode = true
json_default = false
allow_direct_db = false
allow_raw_output = false
`
}

func dataCookie(data map[string]any, key string) string {
	m, _ := data["cookies"].(map[string]string)
	return m[key]
}

func getConfigValue(cfg config.Config, key string) any {
	switch key {
	case "database.path":
		return cfg.Database.Path
	case "auth.dotenv_path":
		return cfg.Auth.DotenvPath
	case "sync.default_count":
		return cfg.Sync.DefaultCount
	case "agent.safe_mode":
		return cfg.Agent.SafeMode
	default:
		if n, err := strconv.Atoi(key); err == nil {
			return n
		}
		return nil
	}
}

func setConfigValue(cfg *config.Config, key, value string) error {
	switch key {
	case "database.path":
		cfg.Database.Path = value
	case "auth.dotenv_path":
		cfg.Auth.DotenvPath = value
	case "sync.default_count":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Sync.DefaultCount = n
	case "sync.request_delay_ms":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Sync.RequestDelayMS = n
	case "agent.safe_mode":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Agent.SafeMode = b
	case "agent.allow_raw_output":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Agent.AllowRawOutput = b
	default:
		return fmt.Errorf("unsupported config key %q", key)
	}
	return nil
}
