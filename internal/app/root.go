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
	root.PersistentFlags().StringSliceVar(&st.authSrc, "auth-source", nil, "override auth source order (repeat or comma-separate: env,dotenv,config,firefox,chrome,macos_keychain)")
	root.PersistentFlags().StringVar(&st.profile, "profile", "", "profile name")
	root.PersistentFlags().BoolVar(&st.quiet, "quiet", false, "reduce human output")
	root.PersistentFlags().BoolVar(&st.verbose, "verbose", false, "increase diagnostics")
	root.PersistentFlags().BoolVar(&st.noColor, "no-color", false, "disable color")

	addCommands(root, st)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		if err, ok := err.(preformattedExitError); ok {
			return err.code
		}
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
	root.AddCommand(countCmd(st))
	root.AddCommand(verifyArchiveCmd(st))
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
	var strict bool
	var online bool
	cmd := &cobra.Command{Use: "doctor", RunE: func(cmd *cobra.Command, args []string) error {
		checks := []map[string]any{}
		failed := 0
		add := func(name string, ok bool, msg string) {
			checks = append(checks, map[string]any{"name": name, "ok": ok, "message": msg})
			if !ok {
				failed++
			}
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
			defer func() { _ = s.Close() }()
			if result, err := s.Integrity(cmd.Context()); err != nil {
				add("database_integrity", false, err.Error())
			} else {
				add("database_integrity", result == "ok", result)
			}
			migrationsOK, migrationsMsg := migrationStatus(cmd.Context(), s)
			add("migrations_applied", migrationsOK, migrationsMsg)
			walOK, walMsg := walStatus(cmd.Context(), s, st.cfg.Database.WAL)
			add("wal_mode", walOK, walMsg)
			if runs, err := s.UnresolvedFailedSyncRuns(cmd.Context(), 3); err == nil {
				add("unresolved_sync_failures", len(runs) == 0, fmt.Sprintf("%d unresolved failed run(s)", len(runs)))
			}
		}
		exportOK, exportMsg := exportDirStatus(st.cfg)
		add("export_directory", exportOK, exportMsg)
		lockOK, lockMsg := activeLockStatus(st.cfg)
		add("active_lock", lockOK, lockMsg)
		cookies, _, _ := auth.Resolve(cmd.Context(), st.cfg)
		authOK, authMsg := auth.ShapeStatus(cookies)
		add("auth_cookies", authOK, authMsg)
		if online {
			ok, msg := doctorAuthOnlineStatus(cmd.Context(), st.cfg)
			add("auth_online", ok, msg)
		}
		if info, err := os.Stat(config.Expand(st.cfg.Auth.DotenvPath)); err == nil {
			add("dotenv_permissions", info.Mode().Perm()&0o077 == 0, fmt.Sprintf("mode %03o", info.Mode().Perm()))
		} else {
			add("dotenv_permissions", false, "dotenv not found")
		}
		fallbackOK, fallbackMsg := queryIDFallbackStatus()
		add("query_id_fallbacks", fallbackOK, fallbackMsg)
		cacheOK, cacheMsg := queryIDCacheStatus()
		add("query_id_cache", cacheOK, cacheMsg)
		remoteOK, remoteMsg := gitRemoteStatus(cmd.Context())
		add("git_remote", remoteOK, remoteMsg)
		dockerOK, dockerMsg := dockerDaemonStatus(cmd.Context())
		add("docker_daemon", dockerOK, dockerMsg)
		data := map[string]any{"checks": checks, "failed_count": failed}
		if st.json {
			if strict && failed > 0 {
				writeJSONErrorWithData(os.Stdout, "doctor", st.started, data, "DOCTOR_CHECK_FAILED", "one or more doctor checks failed", false)
			} else {
				writeJSON(os.Stdout, "doctor", st.started, data)
			}
		} else {
			for _, c := range checks {
				human(os.Stdout, "%v %s: %s", c["ok"], c["name"], c["message"])
			}
		}
		if strict && failed > 0 {
			return preformattedExitError{code: 1}
		}
		return nil
	}}
	cmd.Flags().BoolVar(&strict, "strict", false, "exit nonzero when any doctor check fails")
	cmd.Flags().BoolVar(&online, "online", false, "include live X auth validation")
	return cmd
}

func doctorAuthOnlineStatus(ctx context.Context, cfg config.Config) (bool, string) {
	src, status, err := viewerAuthStatus(ctx, cfg)
	if err != nil {
		return false, classifyCode(err)
	}
	return status >= 200 && status < 300, fmt.Sprintf("source=%s http=%d", src.Name, status)
}

func migrationStatus(ctx context.Context, s *store.Store) (bool, string) {
	var count, maxVersion int
	err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&count, &maxVersion)
	if err != nil {
		return false, err.Error()
	}
	if count < 5 || maxVersion < 5 {
		return false, fmt.Sprintf("versions=%d max=%d, expected >=5", count, maxVersion)
	}
	return true, fmt.Sprintf("versions=%d max=%d", count, maxVersion)
}

func walStatus(ctx context.Context, s *store.Store, expected bool) (bool, string) {
	var mode string
	if err := s.DB().QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return false, err.Error()
	}
	if expected && !strings.EqualFold(mode, "wal") {
		return false, mode
	}
	return true, mode
}

func exportDirStatus(cfg config.Config) (bool, string) {
	path := config.Expand(cfg.Export.Dir)
	if err := os.MkdirAll(path, 0o700); err != nil {
		return false, err.Error()
	}
	probe, err := os.CreateTemp(path, ".xvault-write-test-*")
	if err != nil {
		return false, err.Error()
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return false, err.Error()
	}
	if err := os.Remove(name); err != nil {
		return false, err.Error()
	}
	return true, path
}

func lockPath() string {
	return config.Expand("~/.local/state/xvault/locks/xvault.lock")
}

func acquireOperationLock(cfg config.Config) (func(), error) {
	_ = cfg
	path := lockPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			msg := "another xvault operation is running"
			if b, readErr := os.ReadFile(path); readErr == nil && len(strings.TrimSpace(string(b))) > 0 {
				msg += ": " + strings.TrimSpace(string(b))
			}
			return nil, errCode("LOCKED", msg)
		}
		return nil, err
	}
	payload, _ := json.Marshal(map[string]any{
		"pid":        os.Getpid(),
		"started_at": time.Now().UTC().Format(time.RFC3339),
		"path":       path,
	})
	if _, err := f.Write(append(payload, '\n')); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return func() { _ = os.Remove(path) }, nil
}

func activeLockStatus(cfg config.Config) (bool, string) {
	_ = cfg
	path := lockPath()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, "none"
		}
		return false, err.Error()
	}
	msg := strings.TrimSpace(string(b))
	if msg == "" {
		msg = path
	}
	return false, msg
}

func queryIDFallbackStatus() (bool, string) {
	cache := queryids.Load("")
	missing := []string{}
	for _, op := range queryids.RequiredOperations() {
		if cache.QueryID(op) == "" {
			missing = append(missing, op)
		}
	}
	if len(missing) > 0 {
		return false, "missing fallback query IDs: " + strings.Join(missing, ", ")
	}
	return true, fmt.Sprintf("%d required operation fallback(s) available", len(queryids.RequiredOperations()))
}

func queryIDCacheStatus() (bool, string) {
	path := config.Expand(queryids.DefaultCachePath)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "cache not found"
		}
		return false, err.Error()
	}
	cache := queryids.Load(path)
	updatedAt := info.ModTime().UTC()
	if cache.UpdatedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, cache.UpdatedAt); err == nil {
			updatedAt = parsed
		}
	}
	ttl := cache.TTLHours
	if ttl <= 0 {
		ttl = 24
	}
	age := time.Since(updatedAt)
	if age > time.Duration(ttl)*time.Hour {
		return false, fmt.Sprintf("cache stale: updated_at=%s ttl_hours=%d", updatedAt.Format(time.RFC3339), ttl)
	}
	return true, fmt.Sprintf("fresh: updated_at=%s ttl_hours=%d operations=%d", updatedAt.Format(time.RFC3339), ttl, len(cache.Operations))
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
		cookies, src, err := auth.Resolve(cmd.Context(), st.cfg)
		dotenvPath := config.Expand(st.cfg.Auth.DotenvPath)
		data := map[string]any{"cookies": auth.Status(cmd.Context(), st.cfg), "source": src.Name, "dotenv_path": dotenvPath}
		if err != nil {
			data["source"] = ""
		}
		shapeOK, shapeMsg := auth.ShapeStatus(cookies)
		data["valid_shape"] = shapeOK
		data["shape_message"] = shapeMsg
		if info, statErr := os.Stat(dotenvPath); statErr == nil {
			data["dotenv_exists"] = true
			data["dotenv_mode"] = fmt.Sprintf("%03o", info.Mode().Perm())
			data["dotenv_modified_at"] = info.ModTime().UTC().Format(time.RFC3339)
		} else {
			data["dotenv_exists"] = false
		}
		if st.json {
			writeJSON(os.Stdout, "auth status", st.started, data)
		} else {
			human(os.Stdout, "auth_token=%s ct0=%s twid=%s valid_shape=%t", dataCookie(data, "auth_token"), dataCookie(data, "ct0"), dataCookie(data, "twid"), shapeOK)
		}
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error {
		src, status, err := viewerAuthStatus(cmd.Context(), st.cfg)
		if err != nil {
			return err
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
		if ok, msg := auth.ShapeStatus(c); !ok {
			return errCode("AUTH_MALFORMED", msg)
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

func viewerAuthStatus(ctx context.Context, cfg config.Config) (auth.Source, int, error) {
	c, src, err := auth.Resolve(ctx, cfg)
	if err != nil {
		return auth.Source{}, 0, err
	}
	if ok, msg := auth.ShapeStatus(c); !ok {
		return src, 0, errCode("AUTH_MALFORMED", msg)
	}
	authCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	x := client.New(client.Options{Auth: c, MaxRetries: cfg.Sync.MaxRetries})
	raw, status, err := x.FetchGraphQL(authCtx, client.Operation{
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
		return src, status, err
	}
	var body struct {
		Errors []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return src, status, err
	}
	for _, gqlErr := range body.Errors {
		if gqlErr.Code == 32 || gqlErr.Code == 215 || strings.Contains(strings.ToLower(gqlErr.Message), "not authenticated") {
			return src, status, errCode("AUTH_EXPIRED", "authentication cookies were rejected by X")
		}
	}
	return src, status, nil
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
	var all, full, withThreads, refreshThreads bool
	var folder, threadMode string
	var threadLimit, feedHours int
	cmd := &cobra.Command{Use: "sync [collection]", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		releaseLock, err := acquireOperationLock(st.cfg)
		if err != nil {
			return err
		}
		defer releaseLock()
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
		defer func() { _ = s.Close() }()
		x := client.New(client.Options{Auth: cookies, MaxRetries: st.cfg.Sync.MaxRetries})
		sy := syncer.New(x, s, queryids.Load(""), dbPath, cookies.TWID, time.Duration(st.cfg.Sync.RequestDelayMS)*time.Millisecond)
		results := []syncer.Result{}
		threadsExpanded := 0
		threadsSkipped := 0
		countChanged := cmd.Flags().Changed("count")
		threadLimitChanged := cmd.Flags().Changed("thread-limit")
		expansionLimit := syncThreadLimit(st.cfg, threadMode, threadLimit, threadLimitChanged)
		for _, col := range collections {
			reqCount, reqAll := syncCountForCollection(st.cfg, col, count, all, countChanged)
			folderID := ""
			if col == "bookmarks" && folder != "" {
				if id, ok, err := s.BookmarkFolderIDByName(cmd.Context(), folder); err != nil {
					return err
				} else if ok {
					folderID = id
				}
			}
			res, err := sy.Sync(cmd.Context(), syncer.Request{Collection: col, Count: reqCount, MaxPages: maxPages, All: reqAll, Full: full, Folder: folder, FolderID: folderID, FeedHours: feedHours})
			results = append(results, res)
			if err != nil {
				return err
			}
			if withThreads {
				ids, err := s.CollectionTweetIDs(cmd.Context(), col, expansionLimit)
				if err != nil {
					return err
				}
				for _, id := range ids {
					shouldExpand, err := s.ShouldExpandThread(cmd.Context(), id, threadMode, expansionLimit, refreshThreads)
					if err != nil {
						return err
					}
					if !shouldExpand {
						threadsSkipped++
						continue
					}
					if err := expandTweetDetail(cmd.Context(), st, s, id, threadMode); err != nil {
						return err
					}
					if _, err := s.Thread(cmd.Context(), id, threadMode, expansionLimit); err != nil {
						return err
					}
					threadsExpanded++
				}
			}
		}
		data := map[string]any{"results": results, "with_threads": withThreads, "threads_expanded": threadsExpanded, "threads_skipped": threadsSkipped, "refresh_threads": refreshThreads, "thread_mode": threadMode, "thread_limit": expansionLimit}
		if len(results) == 1 {
			data = map[string]any{"result": results[0], "with_threads": withThreads, "threads_expanded": threadsExpanded, "threads_skipped": threadsSkipped, "refresh_threads": refreshThreads, "thread_mode": threadMode, "thread_limit": expansionLimit}
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
	cmd.PersistentFlags().BoolVar(&refreshThreads, "refresh-threads", false, "refresh existing thread records")
	cmd.PersistentFlags().StringVar(&threadMode, "thread-mode", "thread", "thread expansion mode")
	cmd.PersistentFlags().IntVar(&threadLimit, "thread-limit", 200, "thread expansion limit")
	cmd.PersistentFlags().IntVar(&feedHours, "hours", st.cfg.Sync.FeedDefaultHours, "feed lookback hours")
	var runsCollection, runsStatus string
	var runsLimit int
	runsCmd := &cobra.Command{Use: "runs", RunE: func(c *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer func() { _ = s.Close() }()
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
		defer func() { _ = s.Close() }()
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
		defer func() { _ = s.Close() }()
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
	cmd.AddCommand(&cobra.Command{Use: "summary", RunE: func(c *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer func() { _ = s.Close() }()
		bookmarks, err := s.CollectionCount(c.Context(), "bookmarks")
		if err != nil {
			return err
		}
		likes, err := s.CollectionCount(c.Context(), "likes")
		if err != nil {
			return err
		}
		runs, err := s.ListSyncRuns(c.Context(), "all", "all", 5)
		if err != nil {
			return err
		}
		last := map[string]any{}
		if run, ok, err := s.LastSuccessfulSync(c.Context(), "bookmarks"); err != nil {
			return err
		} else if ok {
			last["bookmarks"] = run
		}
		if run, ok, err := s.LastSuccessfulSync(c.Context(), "likes"); err != nil {
			return err
		} else if ok {
			last["likes"] = run
		}
		data := map[string]any{"bookmarks_count": bookmarks, "likes_count": likes, "recent_runs": runs, "last_successful_sync": last}
		if st.json {
			writeJSON(os.Stdout, "sync summary", st.started, data)
		} else {
			human(os.Stdout, "bookmarks=%d likes=%d recent_runs=%d", bookmarks, likes, len(runs))
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

func syncThreadLimit(cfg config.Config, mode string, flagLimit int, limitChanged bool) int {
	if limitChanged {
		return flagLimit
	}
	if mode == "conversation" && cfg.Sync.DefaultConversationLimit > 0 {
		return cfg.Sync.DefaultConversationLimit
	}
	if cfg.Sync.DefaultThreadLimit > 0 {
		return cfg.Sync.DefaultThreadLimit
	}
	return flagLimit
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
		defer func() { _ = s.Close() }()
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
		defer func() { _ = s.Close() }()
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

func countCmd(st *state) *cobra.Command {
	return &cobra.Command{Use: "count COLLECTION", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer func() { _ = s.Close() }()
		count, err := s.CollectionCount(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		data := map[string]any{"collection": args[0], "count": count}
		if st.json {
			writeJSON(os.Stdout, "count", st.started, data)
		} else {
			human(os.Stdout, "%d", count)
		}
		return nil
	}}
}

func verifyArchiveCmd(st *state) *cobra.Command {
	return &cobra.Command{Use: "verify-archive", RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer func() { _ = s.Close() }()
		integrity, err := s.Integrity(cmd.Context())
		if err != nil {
			return err
		}
		bookmarkCount, err := s.CollectionCount(cmd.Context(), "bookmarks")
		if err != nil {
			return err
		}
		likeCount, err := s.CollectionCount(cmd.Context(), "likes")
		if err != nil {
			return err
		}
		bookmarkRows, err := s.SearchWithFilters(cmd.Context(), "", "bookmarks", "", "", "", "", false, false, 1, 0)
		if err != nil {
			return err
		}
		likeRows, err := s.SearchWithFilters(cmd.Context(), "", "likes", "", "", "", "", false, false, 1, 0)
		if err != nil {
			return err
		}
		ftsRows, err := s.SearchWithFilters(cmd.Context(), "the", "all", "", "", "", "", false, false, 1, 0)
		if err != nil {
			return err
		}
		data := map[string]any{
			"integrity":           integrity,
			"bookmarks_count":     bookmarkCount,
			"likes_count":         likeCount,
			"bookmarks_queryable": len(bookmarkRows) > 0,
			"likes_queryable":     len(likeRows) > 0,
			"fts_queryable":       len(ftsRows) > 0,
			"ok":                  integrity == "ok" && bookmarkCount > 0 && likeCount > 0 && len(bookmarkRows) > 0 && len(likeRows) > 0 && len(ftsRows) > 0,
		}
		if len(bookmarkRows) > 0 {
			data["sample_bookmark_tweet_id"] = bookmarkRows[0].TweetID
		}
		if len(likeRows) > 0 {
			data["sample_like_tweet_id"] = likeRows[0].TweetID
		}
		if data["ok"] != true {
			return errCode("ARCHIVE_VERIFY_FAILED", "local archive verification failed")
		}
		if st.json {
			writeJSON(os.Stdout, "verify-archive", st.started, data)
		} else {
			human(os.Stdout, "bookmarks=%d likes=%d integrity=%s", bookmarkCount, likeCount, integrity)
		}
		return nil
	}}
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
		defer func() { _ = s.Close() }()
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
		defer func() { _ = s.Close() }()
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
		defer func() { _ = s.Close() }()
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
		defer func() { _ = s.Close() }()
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
		defer func() { _ = s.Close() }()
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
			releaseLock, err := acquireOperationLock(st.cfg)
			if err != nil {
				return err
			}
			defer releaseLock()
			s, err := store.Open(config.Expand(st.cfg.Database.Path))
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
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
		releaseLock, err := acquireOperationLock(st.cfg)
		if err != nil {
			return err
		}
		defer releaseLock()
		collection, _ := cmd.Flags().GetString("collection")
		folder, _ := cmd.Flags().GetString("folder")
		output, _ := cmd.Flags().GetString("output")
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer func() { _ = s.Close() }()
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
	var htmlCollection, htmlFolder, htmlOutput string
	var htmlFailOnLarge bool
	htmlCmd := &cobra.Command{Use: "html", RunE: func(cmd *cobra.Command, args []string) error {
		releaseLock, err := acquireOperationLock(st.cfg)
		if err != nil {
			return err
		}
		defer releaseLock()
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer func() { _ = s.Close() }()
		data, err := exporter.HTMLWithFolderOptions(cmd.Context(), s, htmlCollection, htmlFolder, htmlOutput, st.cfg.Export.HTMLWarnSizeMB, htmlFailOnLarge)
		if err != nil {
			return err
		}
		if st.json {
			writeJSON(os.Stdout, "export html", st.started, data)
		} else {
			if data["large_file_warning"] == true {
				human(os.Stderr, "warning: HTML export is larger than %d MiB", st.cfg.Export.HTMLWarnSizeMB)
			}
			human(os.Stdout, "exported %s", data["output"])
		}
		return nil
	}}
	htmlCmd.Flags().StringVar(&htmlCollection, "collection", "all", "collection")
	htmlCmd.Flags().StringVar(&htmlFolder, "folder", "", "bookmark folder")
	htmlCmd.Flags().StringVar(&htmlOutput, "output", "", "output path")
	htmlCmd.Flags().BoolVar(&htmlFailOnLarge, "fail-on-large", false, "fail if estimated HTML size exceeds export.html_warn_size_mb")
	cmd.AddCommand(htmlCmd)
	var markdownCollection, markdownFolder, markdownOutput, markdownMode string
	markdownCmd := &cobra.Command{Use: "markdown", RunE: func(cmd *cobra.Command, args []string) error {
		releaseLock, err := acquireOperationLock(st.cfg)
		if err != nil {
			return err
		}
		defer releaseLock()
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer func() { _ = s.Close() }()
		var data map[string]any
		switch markdownMode {
		case "files":
			data, err = exporter.MarkdownWithFolder(cmd.Context(), s, markdownCollection, markdownFolder, markdownOutput, false)
		case "single":
			data, err = exporter.MarkdownSingleWithFolder(cmd.Context(), s, markdownCollection, markdownFolder, markdownOutput)
		default:
			return errCode("INVALID_ARGUMENT", "markdown mode must be single or files")
		}
		if err != nil {
			return err
		}
		if st.json {
			writeJSON(os.Stdout, "export markdown", st.started, data)
		} else {
			human(os.Stdout, "exported %s", data["output"])
		}
		return nil
	}}
	markdownCmd.Flags().StringVar(&markdownCollection, "collection", "all", "collection")
	markdownCmd.Flags().StringVar(&markdownFolder, "folder", "", "bookmark folder")
	markdownCmd.Flags().StringVar(&markdownOutput, "output", "", "output path")
	markdownCmd.Flags().StringVar(&markdownMode, "mode", "files", "markdown mode: single or files")
	cmd.AddCommand(markdownCmd)
	addExport("hermes", func(ctx context.Context, s *store.Store, c, f, o string) (map[string]any, error) {
		if c == "" {
			c = "all"
		}
		return exporter.MarkdownWithFolder(ctx, s, c, f, o, true)
	})
	var obsidianCollection, obsidianFolder, obsidianOutput string
	var obsidianWithIndex bool
	obsidianCmd := &cobra.Command{Use: "obsidian", RunE: func(cmd *cobra.Command, args []string) error {
		releaseLock, err := acquireOperationLock(st.cfg)
		if err != nil {
			return err
		}
		defer releaseLock()
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer func() { _ = s.Close() }()
		data, err := exporter.ObsidianWithFolder(cmd.Context(), s, obsidianCollection, obsidianFolder, obsidianOutput, obsidianWithIndex)
		if err != nil {
			return err
		}
		if st.json {
			writeJSON(os.Stdout, "export obsidian", st.started, data)
		} else {
			human(os.Stdout, "exported %s", data["output"])
		}
		return nil
	}}
	obsidianCmd.Flags().StringVar(&obsidianCollection, "collection", "all", "collection")
	obsidianCmd.Flags().StringVar(&obsidianFolder, "folder", "", "bookmark folder")
	obsidianCmd.Flags().StringVar(&obsidianOutput, "output", "", "output path")
	obsidianCmd.Flags().BoolVar(&obsidianWithIndex, "with-index-jsonl", false, "also write index.jsonl")
	cmd.AddCommand(obsidianCmd)
	return cmd
}

func dbCmd(st *state) *cobra.Command {
	cmd := &cobra.Command{Use: "db"}
	cmd.AddCommand(&cobra.Command{Use: "migrate", RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(config.Expand(st.cfg.Database.Path))
		if err != nil {
			return err
		}
		defer func() { _ = s.Close() }()
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
		defer func() { _ = s.Close() }()
		result, err := s.Integrity(cmd.Context())
		if err != nil {
			return err
		}
		data := map[string]any{"result": result, "ok": result == "ok"}
		if result != "ok" {
			if st.json {
				writeJSONErrorWithData(os.Stdout, "db integrity", st.started, data, "DB_INTEGRITY_FAILED", "database integrity check failed", false)
			}
			return preformattedExitError{code: 1}
		}
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
		defer func() { _ = s.Close() }()
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
		defer func() { _ = s.Close() }()
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
		defer func() { _ = s.Close() }()
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
		defer func() { _ = s.Close() }()
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
		if files == nil {
			files = []string{}
		}
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
		defer func() { _ = db.Close() }()
		var result string
		err = db.QueryRowContext(cmd.Context(), "PRAGMA integrity_check").Scan(&result)
		if err != nil {
			return err
		}
		data := map[string]any{"path": args[0], "result": result, "ok": result == "ok"}
		if result != "ok" {
			if st.json {
				writeJSONErrorWithData(os.Stdout, "backup verify", st.started, data, "BACKUP_VERIFY_FAILED", "backup integrity check failed", false)
			}
			return preformattedExitError{code: 1}
		}
		if st.json {
			writeJSON(os.Stdout, "backup verify", st.started, data)
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
		fmt.Print(`# ~/.config/systemd/user/xvault-bookmarks.service
[Unit]
Description=xvault bookmark sync

[Service]
Type=oneshot
ExecStart=/usr/local/bin/xvault sync bookmarks --count 300 --max-pages 5 --json

# ~/.config/systemd/user/xvault-bookmarks.timer
[Unit]
Description=Run xvault bookmark sync every 3 hours

[Timer]
OnBootSec=10min
OnUnitActiveSec=3h
Persistent=true

[Install]
WantedBy=timers.target

# ~/.config/systemd/user/xvault-likes.service
[Unit]
Description=xvault likes sync

[Service]
Type=oneshot
ExecStart=/usr/local/bin/xvault sync likes --count 300 --max-pages 5 --json

# ~/.config/systemd/user/xvault-likes.timer
[Unit]
Description=Run xvault likes sync every 6 hours

[Timer]
OnBootSec=20min
OnUnitActiveSec=6h
Persistent=true

[Install]
WantedBy=timers.target
`)
	}})
	sys.PersistentFlags().BoolVar(&user, "user", false, "print user service example")
	cmd.AddCommand(sys)
	cron := &cobra.Command{Use: "cron"}
	cron.AddCommand(&cobra.Command{Use: "print", Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("0 */3 * * * /usr/local/bin/xvault sync bookmarks --count 300 --max-pages 5 --json >> ~/.local/state/xvault/logs/bookmarks.log 2>&1")
		fmt.Println("30 */6 * * * /usr/local/bin/xvault sync likes --count 300 --max-pages 5 --json >> ~/.local/state/xvault/logs/likes.log 2>&1")
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
	if coded, ok := err.(codedError); ok && (coded.code == "AUTH_EXPIRED" || coded.code == "AUTH_MALFORMED") {
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

func retryable(err error) bool {
	if coded, ok := err.(codedError); ok && coded.code == "LOCKED" {
		return true
	}
	return strings.Contains(err.Error(), "rate limited")
}

func sanitizeErr(err error) string {
	msg := err.Error()
	for _, token := range []string{"auth_token", "ct0", "Authorization", "Cookie", "x-csrf-token"} {
		msg = strings.ReplaceAll(msg, token, "[REDACTED]")
	}
	return msg
}

func invokedCommand(args []string) string {
	tokens := commandTokens(args)
	if len(tokens) == 0 {
		return "xvault"
	}
	top := tokens[0]
	if !knownTopCommand(top) {
		return top
	}
	parts := []string{top}
	if len(tokens) > 1 && knownSubcommand(top, tokens[1]) {
		parts = append(parts, tokens[1])
		if len(tokens) > 2 && knownNestedSubcommand(top, tokens[1], tokens[2]) {
			parts = append(parts, tokens[2])
		}
	}
	return strings.Join(parts, " ")
}

func commandTokens(args []string) []string {
	tokens := []string{}
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if !strings.Contains(arg, "=") && !boolFlag(arg) {
				skipNext = true
			}
			continue
		}
		tokens = append(tokens, arg)
	}
	return tokens
}

func knownTopCommand(command string) bool {
	switch command {
	case "auth", "backup", "bookmarks", "completion", "config", "conversation", "count", "db", "doctor", "export", "help", "init", "open", "refresh-ids", "search", "service", "show", "show-url", "stats", "status", "sync", "thread", "vacuum", "verify-archive", "version":
		return true
	default:
		return false
	}
}

func knownSubcommand(command, subcommand string) bool {
	switch command {
	case "auth":
		return inSet(subcommand, "import-browser", "import-env", "sources", "status", "test")
	case "backup":
		return inSet(subcommand, "create", "list", "verify")
	case "bookmarks":
		return subcommand == "folders"
	case "config":
		return inSet(subcommand, "get", "set", "show")
	case "db":
		return inSet(subcommand, "integrity", "migrate", "rebuild-fts")
	case "export":
		return inSet(subcommand, "csv", "hermes", "html", "json", "markdown", "obsidian")
	case "service":
		return inSet(subcommand, "cron", "systemd")
	case "sync":
		return inSet(subcommand, "bookmarks", "checkpoints", "feed", "likes", "posts", "replies", "reposts", "runs", "sanitize-runs", "summary", "tweets")
	default:
		return false
	}
}

func knownNestedSubcommand(command, subcommand, nested string) bool {
	return command == "service" && inSet(subcommand, "cron", "systemd") && nested == "print"
}

func inSet(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

func boolFlag(arg string) bool {
	switch arg {
	case "--json", "--quiet", "--verbose", "--no-color", "--all", "--full", "--with-threads", "--refresh-threads", "--has-media", "--has-link", "--pretty", "--force", "--user", "--recent":
		return true
	default:
		return false
	}
}

type codedError struct{ code, message string }

func (e codedError) Error() string       { return e.message }
func errCode(code, message string) error { return codedError{code: code, message: message} }

type preformattedExitError struct{ code int }

func (e preformattedExitError) Error() string { return "command already reported failure" }

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
	case "sync.default_like_count":
		return cfg.Sync.DefaultLikeCount
	case "sync.default_bookmark_count":
		return cfg.Sync.DefaultBookmarkCount
	case "sync.request_delay_ms":
		return cfg.Sync.RequestDelayMS
	case "sync.max_retries":
		return cfg.Sync.MaxRetries
	case "sync.stop_after_consecutive_rate_limits":
		return cfg.Sync.StopAfterRateLimits
	case "sync.store_raw":
		return cfg.Sync.StoreRaw
	case "sync.checkpoint_every_items":
		return cfg.Sync.CheckpointEveryItems
	case "sync.checkpoint_every_pages":
		return cfg.Sync.CheckpointEveryPages
	case "sync.default_thread_limit":
		return cfg.Sync.DefaultThreadLimit
	case "sync.default_conversation_limit":
		return cfg.Sync.DefaultConversationLimit
	case "sync.feed_default_hours":
		return cfg.Sync.FeedDefaultHours
	case "database.wal":
		return cfg.Database.WAL
	case "database.busy_timeout_ms":
		return cfg.Database.BusyTimeoutMS
	case "database.foreign_keys":
		return cfg.Database.ForeignKeys
	case "export.dir":
		return cfg.Export.Dir
	case "export.markdown_layout":
		return cfg.Export.MarkdownLayout
	case "export.overwrite":
		return cfg.Export.Overwrite
	case "export.html_warn_size_mb":
		return cfg.Export.HTMLWarnSizeMB
	case "agent.safe_mode":
		return cfg.Agent.SafeMode
	case "agent.json_default":
		return cfg.Agent.JSONDefault
	case "agent.allow_direct_db":
		return cfg.Agent.AllowDirectDB
	case "agent.allow_raw_output":
		return cfg.Agent.AllowRawOutput
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
	case "sync.default_like_count":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Sync.DefaultLikeCount = n
	case "sync.default_bookmark_count":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Sync.DefaultBookmarkCount = n
	case "sync.request_delay_ms":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Sync.RequestDelayMS = n
	case "sync.max_retries":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Sync.MaxRetries = n
	case "sync.stop_after_consecutive_rate_limits":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Sync.StopAfterRateLimits = n
	case "sync.store_raw":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Sync.StoreRaw = b
	case "sync.checkpoint_every_items":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Sync.CheckpointEveryItems = n
	case "sync.checkpoint_every_pages":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Sync.CheckpointEveryPages = n
	case "sync.default_thread_limit":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Sync.DefaultThreadLimit = n
	case "sync.default_conversation_limit":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Sync.DefaultConversationLimit = n
	case "sync.feed_default_hours":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Sync.FeedDefaultHours = n
	case "database.wal":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Database.WAL = b
	case "database.busy_timeout_ms":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Database.BusyTimeoutMS = n
	case "database.foreign_keys":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Database.ForeignKeys = b
	case "export.dir":
		cfg.Export.Dir = value
	case "export.markdown_layout":
		cfg.Export.MarkdownLayout = value
	case "export.overwrite":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Export.Overwrite = b
	case "export.html_warn_size_mb":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Export.HTMLWarnSizeMB = n
	case "agent.safe_mode":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Agent.SafeMode = b
	case "agent.json_default":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Agent.JSONDefault = b
	case "agent.allow_direct_db":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Agent.AllowDirectDB = b
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
