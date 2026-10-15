// Command hlctl is the Homelab Control administration CLI.
//
// M0 implements local account management only. Theme and plugin commands
// described in docs/project/06-THEME-AND-PLUGIN-SDK.md are planned (M3).
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"homelab-control/backend/internal/api"
	"homelab-control/backend/internal/auth"
)

const usage = `hlctl - Homelab Control administration CLI (M0)

Usage:
  hlctl user add     --file PATH --username NAME [--scopes system.read]   (password read from stdin)
  hlctl user passwd  --file PATH --username NAME                          (password read from stdin)
  hlctl user disable --file PATH --username NAME
  hlctl user list    --file PATH
  hlctl version
`

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "hlctl:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return errors.New("missing command")
	}
	if args[0] == "version" {
		fmt.Fprintln(stdout, api.Version)
		return nil
	}
	if args[0] != "user" || len(args) < 2 {
		fmt.Fprint(stdout, usage)
		return fmt.Errorf("unknown command %q", strings.Join(args, " "))
	}
	sub := args[1]
	fs := flag.NewFlagSet("user "+sub, flag.ContinueOnError)
	file := fs.String("file", "/etc/homelab-control/users.json", "users file")
	username := fs.String("username", "", "account name")
	scopes := fs.String("scopes", auth.ScopeSystemRead, "comma-separated scopes")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	f, err := loadOrNew(*file, sub == "add")
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		for _, u := range f.Users {
			state := "enabled"
			if u.Disabled {
				state = "disabled"
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", u.Username, state, strings.Join(u.Scopes, ","))
		}
		return nil
	case "add", "passwd", "disable":
	default:
		return fmt.Errorf("unknown user subcommand %q", sub)
	}
	if err := auth.ValidateUsername(*username); err != nil {
		return err
	}
	idx := -1
	for i, u := range f.Users {
		if u.Username == *username {
			idx = i
		}
	}
	switch sub {
	case "add":
		if idx >= 0 {
			return fmt.Errorf("user %q already exists", *username)
		}
		pw, err := readPassword(stdin)
		if err != nil {
			return err
		}
		h, err := auth.HashPassword(pw, auth.DefaultIterations)
		if err != nil {
			return err
		}
		var sc []string
		for _, s := range strings.Split(*scopes, ",") {
			if s = strings.TrimSpace(s); s != "" {
				sc = append(sc, s)
			}
		}
		f.Users = append(f.Users, auth.User{Username: *username, PasswordHash: h, Scopes: sc})
	case "passwd":
		if idx < 0 {
			return fmt.Errorf("no such user %q", *username)
		}
		pw, err := readPassword(stdin)
		if err != nil {
			return err
		}
		if f.Users[idx].PasswordHash, err = auth.HashPassword(pw, auth.DefaultIterations); err != nil {
			return err
		}
	case "disable":
		if idx < 0 {
			return fmt.Errorf("no such user %q", *username)
		}
		f.Users[idx].Disabled = true
	}
	if err := auth.SaveUsersFile(*file, f); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "updated %s (restart hc-api to apply)\n", *file)
	return nil
}

func loadOrNew(path string, allowCreate bool) (*auth.UsersFile, error) {
	f, err := auth.LoadUsersFile(path)
	if err == nil {
		return f, nil
	}
	if allowCreate && errors.Is(err, os.ErrNotExist) {
		return &auth.UsersFile{Version: 1}, nil
	}
	return nil, err
}

// readPassword reads one line from stdin. Interactive no-echo input is left to
// the caller's shell (e.g. `read -rs`), keeping hlctl free of terminal and
// process-execution dependencies.
func readPassword(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	pw := strings.TrimRight(line, "\r\n")
	if len(pw) < auth.MinPasswordLength {
		return "", fmt.Errorf("password must be at least %d characters", auth.MinPasswordLength)
	}
	return pw, nil
}
