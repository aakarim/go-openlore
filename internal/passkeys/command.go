package passkeys

import (
	"fmt"
	"io"
	"strings"

	"github.com/aakarim/go-openlore/pkg/shell/cmds"
)

const passkeyHelp = `
  passkey — Manage WebAuthn passkeys for HTTP documentation access

  Passkeys let humans browse your lore in a web browser, authenticated
  via WebAuthn (Face ID, Touch ID, security keys). Credentials are
  stored in a plain JSON file that agents can edit directly.

  Usage:
    passkey register [options]    Start passkey registration
    passkey list                  List registered passkeys
    passkey revoke <name>         Remove a passkey by name
    passkey help                  Show this help

  Register options:
    --name NAME    Label for the passkey (default: "default")
    --lore SPEC    Lore spec to grant (default: "full-access")

  Examples:
    passkey register                              Register with defaults
    passkey register --name "MacBook" --lore ops  Register for "ops" lore
    passkey list                                  Show all passkeys
    passkey revoke "MacBook"                      Delete a passkey

  Setup:
    Add this to your openlore.yml to enable passkeys:

      passkeys:
        enabled: true
        rp_id: localhost                    # your domain
        rp_origins: ["http://localhost:8080"]
        lore_path: /lore

    Then run 'passkey register' and open the printed URL in a browser.
    After registering, visit /lore/ to browse docs with your passkey.
`

// RegisterShellCommand registers the `passkey` shell command backed by the
// given Passkeys instance. baseURL is the public HTTP origin used to build the
// registration link printed to the agent.
func RegisterShellCommand(pk *Passkeys, baseURL string) {
	baseURL = strings.TrimRight(baseURL, "/")
	cmds.Register("passkey", func(ctx cmds.CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
		if len(args) == 0 {
			fmt.Fprint(w, passkeyHelp)
			return 0
		}

		switch args[0] {
		case "help", "-h", "--help":
			fmt.Fprint(w, passkeyHelp)
			return 0
		case "register":
			return pk.cmdRegister(baseURL, args[1:], w, errW)
		case "list":
			return pk.cmdList(w)
		case "revoke":
			return pk.cmdRevoke(args[1:], w, errW)
		default:
			fmt.Fprintf(errW, "passkey: unknown subcommand %q\n", args[0])
			fmt.Fprintln(errW, "Run 'passkey help' for usage.")
			return 1
		}
	})
}

func (pk *Passkeys) cmdRegister(baseURL string, args []string, w io.Writer, errW io.Writer) int {
	name := "default"
	lore := "full-access"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name":
			if i+1 < len(args) {
				name = args[i+1]
				i++
			}
		case "--lore":
			if i+1 < len(args) {
				lore = args[i+1]
				i++
			}
		}
	}

	pr, err := pk.pending.Create(lore, name)
	if err != nil {
		fmt.Fprintf(errW, "passkey: %s\n", err)
		return 1
	}

	url := fmt.Sprintf("%s/passkey/r/%s", baseURL, pr.Token)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  📱 Passkey Registration")
	fmt.Fprintln(w, "  ─────────────────────────")
	fmt.Fprintf(w, "  Name:    %s\n", name)
	fmt.Fprintf(w, "  Access:  %s\n", lore)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Visit this URL to register your passkey:")
	fmt.Fprintf(w, "  %s\n", url)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  ⏱  This link expires in 5 minutes.")
	fmt.Fprintln(w)
	return 0
}

func (pk *Passkeys) cmdList(w io.Writer) int {
	creds := pk.store.AllCredentials()
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %-20s %-16s %s\n", "NAME", "ACCESS", "REGISTERED")
	for _, c := range creds {
		fmt.Fprintf(w, "  %-20s %-16s %s\n", c.Name, c.Lore, c.CreatedAt.Format("2006-01-02 15:04"))
	}
	fmt.Fprintln(w)
	return 0
}

func (pk *Passkeys) cmdRevoke(args []string, w io.Writer, errW io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errW, "passkey: revoke requires a name")
		return 1
	}
	name := args[0]
	removed, err := pk.store.Remove(name)
	if err != nil {
		fmt.Fprintf(errW, "passkey: %s\n", err)
		return 1
	}
	if !removed {
		fmt.Fprintf(errW, "passkey: no passkey named %q\n", name)
		return 1
	}
	fmt.Fprintf(w, "Revoked passkey %q\n", name)
	return 0
}
