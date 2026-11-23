//go:build windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	workerTaskName   = "PlexMediaManager-Worker"
	webTaskName      = "PlexMediaManager-Web"
	firewallRuleName = "PlexMediaManager-Web-8765"
)

func taskArguments(binPath, subcommand string) []string {
	quoted := "\"" + binPath + "\" " + subcommand
	return []string{
		"/Create",
		"/TN", taskNameForSubcommand(subcommand),
		"/SC", "ONLOGON",
		"/TR", quoted,
		"/RL", "LIMITED",
		"/F",
	}
}

func taskNameForSubcommand(subcommand string) string {
	if subcommand == "web" {
		return webTaskName
	}
	return workerTaskName
}

func runSchtasks(args ...string) error {
	out, err := exec.Command("schtasks.exe", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks %v: %w: %s", args, err, string(out))
	}
	return nil
}

func Install() error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	bin, err = filepath.Abs(bin)
	if err != nil {
		return err
	}

	if err := runSchtasks(taskArguments(bin, "worker")...); err != nil {
		return err
	}
	if err := runSchtasks(taskArguments(bin, "web")...); err != nil {
		return err
	}
	if out, err := exec.Command(
		"netsh.exe", "advfirewall", "firewall", "add", "rule",
		"name="+firewallRuleName,
		"dir=in",
		"action=allow",
		"protocol=TCP",
		"localport=8765",
	).CombinedOutput(); err != nil {
		return fmt.Errorf("create firewall rule: %w: %s", err, string(out))
	}
	if out, err := exec.Command("schtasks.exe", "/Run", "/TN", workerTaskName).CombinedOutput(); err != nil {
		return fmt.Errorf("start worker task: %w: %s", err, string(out))
	}
	if out, err := exec.Command("schtasks.exe", "/Run", "/TN", webTaskName).CombinedOutput(); err != nil {
		return fmt.Errorf("start web task: %w: %s", err, string(out))
	}

	fmt.Printf("Installed and started: %s, %s\n", workerTaskName, webTaskName)
	return nil
}

func Uninstall() error {
	for _, name := range []string{workerTaskName, webTaskName} {
		_ = exec.Command("schtasks.exe", "/End", "/TN", name).Run()
		out, err := exec.Command("schtasks.exe", "/Delete", "/TN", name, "/F").CombinedOutput()
		if err != nil {
			lower := string(out)
			if len(lower) == 0 {
				continue
			}
		}
		_ = exec.Command(
			"netsh.exe", "advfirewall", "firewall", "delete", "rule",
			"name="+firewallRuleName,
		).Run()
	}
	fmt.Printf("Uninstalled: %s, %s\n", workerTaskName, webTaskName)
	return nil
}
