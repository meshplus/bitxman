package fabric

import (
	"bufio"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/meshplus/goduck/repo"
	"github.com/urfave/cli/v2"
)

func GetFabricCMD() *cli.Command {
	return &cli.Command{
		Name:  "fabric",
		Usage: "operation about fabric network",
		Subcommands: []*cli.Command{
			{
				Name:   "start",
				Usage:  "start a fabric network",
				Action: start,
			},
			{
				Name:  "chaincode",
				Usage: "deploy chaincode on your network",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "config",
						Usage:    "specify fabric network config file path",
						Required: true,
					},
				},
				Action: installChaincode,
			},
		},
	}
}

func start(ctx *cli.Context) error {
	repoRoot, err := repo.PathRootWithDefault(ctx.String("repo"))
	if err != nil {
		return err
	}

	args := []string{filepath.Join(repoRoot, "ffn.sh"), "up"}

	return execCmd(args)
}

func installChaincode(ctx *cli.Context) error {
	repoRoot, err := repo.PathRootWithDefault(ctx.String("repo"))
	if err != nil {
		return err
	}

	fabricConfig := ctx.String("config")
	args := make([]string, 0)
	args = append(args, filepath.Join(repoRoot, "chaincode.sh"), "install", "-c", fabricConfig)

	return execCmd(args)
}

func execCmd(args []string) error {
	cmd := exec.Command("/bin/bash", args...)
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("execute command: %s", err.Error())
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		m := scanner.Text()
		fmt.Println(m)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("wait for command to finish: %s", err.Error())
	}
	return nil
}
