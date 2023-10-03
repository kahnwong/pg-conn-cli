package cmd

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

func connectionInfoGet(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	var autocompleteOptions []string

	if len(args) == 0 { // databases
		autocompleteOptions = getDatabases()
	} else if len(args) == 1 { // roles
		autocompleteOptions = getRoles(args[0])
	}

	return autocompleteOptions, cobra.ShellCompDirectiveNoFileComp
}

var connectCmd = &cobra.Command{
	Use:               "connect [database] [role]",
	Short:             "Connect to a database with specified role",
	Long:              `Connect to a database with specified role`,
	ValidArgsFunction: connectionInfoGet,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			fmt.Println("Please specify a database and role")
			os.Exit(1)
		} else if len(args) == 1 {
			fmt.Println("Please specify a role")
			os.Exit(1)
		}

		// get db config
		dbConfig := getConnectionInfo(args[0], args[1])

		// start proxy process if necessary
		var proxyCmd *exec.Cmd
		if dbConfig.ProxyKind != "" {
			proxyCmd = createProxy(dbConfig)
		}

		// connect via pgcli
		dbCmd := connectDb(dbConfig)

		time.Sleep(1 * time.Second) // important, so proxy has some time to start up

		if err := dbCmd.Run(); err != nil {
			fmt.Printf("Failed to start the second process: %v\n", err)
			os.Exit(1)
		}

		// clean up proxy PID
		if dbConfig.ProxyKind != "" {
			pgid, err := syscall.Getpgid(proxyCmd.Process.Pid)
			if err == nil {
				err = syscall.Kill(-pgid, syscall.SIGKILL)
				if err != nil {
					log.Fatal(err)
				}
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(connectCmd)
}

type ConnectCommand struct{}

func (c *ConnectCommand) Help() string {
	return "Connect to a database"
}

func (c *ConnectCommand) Synopsis() string {
	return "Connect to a database"
}

type Connection struct {
	Hostname  string
	Username  string
	Password  string
	Dbname    string
	ProxyKind string
	ProxyHost string
}

func getConnectionInfo(name string, role string) Connection {
	var dbConfig Connection

	for _, db := range config {
		if db.Name == name {
			dbConfig.Hostname = db.Hostname
			dbConfig.ProxyKind = db.Proxy.Kind
			dbConfig.ProxyHost = db.Proxy.Host

			for _, dbRole := range db.Roles {
				if dbRole.Username == role {
					dbConfig.Username = dbRole.Username
					dbConfig.Password = dbRole.Password
					dbConfig.Dbname = dbRole.Dbname
				}
			}
		}
	}

	return dbConfig
}

func createProxy(c Connection) *exec.Cmd {
	var proxyCmd string
	if c.ProxyKind == "ssh" {
		proxyCmd = fmt.Sprintf("ssh -N -L 5432:%s:5432 %s", c.Hostname, c.ProxyHost)
	} else if c.ProxyKind == "cloud-sql-proxy" {
		proxyCmd = fmt.Sprintf("cloud-sql-proxy %s --quiet", c.ProxyHost)
	}

	cmd := exec.Command("/bin/sh", "-c", proxyCmd)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		fmt.Printf("Failed to start the first process: %v\n", err)
		os.Exit(1)
	}

	return cmd
}

func connectDb(c Connection) *exec.Cmd {
	// set hostname
	var connectHostname string
	if c.ProxyKind != "" {
		if c.ProxyKind == "ssh" {
			connectHostname = "localhost"
		} else if c.ProxyKind == "cloud-sql-proxy" {
			connectHostname = "127.0.0.1"
		}
	} else {
		connectHostname = c.Hostname
	}

	// connect
	connectionString := fmt.Sprintf("postgresql://%s:%s@%s:5432/%s?sslmode=disable", c.Username, c.Password, connectHostname, c.Dbname)
	cmd := exec.Command("/bin/sh", "-c", fmt.Sprintf("pgcli \"%s\"", connectionString))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd
}
