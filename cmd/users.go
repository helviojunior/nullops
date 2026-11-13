package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/helviojunior/fmweb/internal/ascii"
	"github.com/helviojunior/fmweb/pkg/auth"
	"github.com/helviojunior/fmweb/pkg/log"
	"github.com/helviojunior/fmweb/pkg/session"
	"github.com/spf13/cobra"
)

var listUsersCmd = &cobra.Command{
	Use:     "list-users",
	Short:   "List the users allowed to sign in",
	Long:    ascii.LogoHelp(`List the users in <data-dir>/users.json (no secrets are shown).`),
	Example: "\n   - fmweb list-users",
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := dataDir()
		if err != nil {
			return err
		}
		store, err := auth.Open(d.UsersFile())
		if err != nil {
			return err
		}
		users := store.List()
		if len(users) == 0 {
			fmt.Println("no users (run `fmweb add-user -u <name>`)")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "USERNAME\tACCESS\tMFA\tCREATED\tUPDATED")
		for _, u := range users {
			access, mfa := "read-write", "no"
			if u.ReadOnly {
				access = "read-only"
			}
			if u.HasMFA() {
				mfa = "yes"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", u.Username, access, mfa,
				u.CreatedAt.Local().Format("2006-01-02 15:04"), u.UpdatedAt.Local().Format("2006-01-02 15:04"))
		}
		return w.Flush()
	},
}

var delUserCmd = &cobra.Command{
	Use:     "del-user <username>",
	Short:   "Delete a user and revoke its sessions",
	Long:    ascii.LogoHelp(`Delete a user from <data-dir>/users.json and revoke every session it has.`),
	Example: "\n   - fmweb del-user bob",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := dataDir()
		if err != nil {
			return err
		}
		store, err := auth.Open(d.UsersFile())
		if err != nil {
			return err
		}
		if err := store.Delete(args[0]); err != nil {
			if errors.Is(err, auth.ErrNoUser) {
				return fmt.Errorf("user %q not found", args[0])
			}
			return err
		}
		n := int64(0)
		if st, err := session.OpenSQLite(d.SessionsFile()); err == nil {
			n, _ = st.DeleteUser(args[0])
			st.Close()
		}
		log.Info("user deleted", "user", args[0], "sessions_revoked", n)
		return nil
	},
}

var sessOpts = struct {
	User string
	All  bool
}{}

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List or revoke active web sessions",
	Long: ascii.LogoHelp(`List the active sessions stored in <data-dir>/sessions.db, or revoke them.
A running server notices a revoked session within a few seconds.`),
	Example: `
   - fmweb sessions
   - fmweb sessions revoke 3fa9c1
   - fmweb sessions revoke --user bob
   - fmweb sessions revoke --all`,
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := openSessions()
		if err != nil {
			return err
		}
		defer st.Close()
		recs, err := st.List()
		if err != nil {
			return err
		}
		if len(recs) == 0 {
			fmt.Println("no active sessions")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tUSER\tIP\tCREATED\tEXPIRES\tENC\tUSER-AGENT")
		for _, r := range recs {
			enc := "yes"
			if !r.Encrypted {
				enc = "no"
			}
			ua := r.UserAgent
			if len(ua) > 60 {
				ua = ua[:60] + "…"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.IDHash[:12], r.Username, r.IP,
				r.CreatedAt.Local().Format("2006-01-02 15:04"), r.ExpiresAt.Local().Format("2006-01-02 15:04"), enc, ua)
		}
		return w.Flush()
	},
}

var sessionsRevokeCmd = &cobra.Command{
	Use:   "revoke [id-prefix]",
	Short: "Revoke sessions by id prefix, by user, or all of them",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		n := 0
		if len(args) == 1 {
			n++
		}
		if sessOpts.User != "" {
			n++
		}
		if sessOpts.All {
			n++
		}
		if n != 1 {
			return errors.New("use exactly one of: <id-prefix>, --user <name>, --all")
		}
		st, err := openSessions()
		if err != nil {
			return err
		}
		defer st.Close()
		var cnt int64
		switch {
		case sessOpts.All:
			recs, err := st.List()
			if err != nil {
				return err
			}
			for _, r := range recs {
				if c, err := st.DeletePrefix(r.IDHash); err == nil {
					cnt += c
				}
			}
		case sessOpts.User != "":
			cnt, err = st.DeleteUser(sessOpts.User)
		default:
			cnt, err = st.DeletePrefix(strings.ToLower(args[0]))
		}
		if err != nil {
			return err
		}
		log.Info("sessions revoked", "count", cnt)
		return nil
	},
}

func openSessions() (*session.SQLiteStore, error) {
	d, err := dataDir()
	if err != nil {
		return nil, err
	}
	return session.OpenSQLite(d.SessionsFile())
}

func init() {
	rootCmd.AddCommand(listUsersCmd, delUserCmd, sessionsCmd)
	sessionsCmd.AddCommand(sessionsRevokeCmd)
	sessionsRevokeCmd.Flags().StringVar(&sessOpts.User, "user", "", "Revoke every session of this user")
	sessionsRevokeCmd.Flags().BoolVar(&sessOpts.All, "all", false, "Revoke every session")
}
