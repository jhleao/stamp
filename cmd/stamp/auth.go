package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	stampdrive "github.com/jhleao/stamp/internal/drive"
	"github.com/jhleao/stamp/internal/notion"
	"golang.org/x/term"
)

func authProvider(args []string, command string) (string, error) {
	if len(args) == 0 {
		return "drive", nil
	}
	if len(args) == 1 {
		provider := strings.ToLower(args[0])
		if provider == "drive" || provider == "notion" {
			return provider, nil
		}
	}
	return "", fmt.Errorf("usage: stamp %s [drive|notion]", command)
}

func loginCommand(args []string) error {
	provider, err := authProvider(args, "login")
	if err != nil {
		return err
	}
	if provider == "drive" {
		message, err := stampdrive.Login(context.Background())
		if err == nil {
			fmt.Println(message)
		}
		return err
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("run stamp login notion in a terminal for hidden token entry; automation can use STAMP_NOTION_TOKEN")
	}
	fmt.Println("Create a personal access token at https://www.notion.so/developers/tokens")
	fmt.Println("Choose your workspace and enable Notion API. Your account needs edit access to the project and its subpages.")
	fmt.Print("Notion token (hidden): ")
	secret, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return errors.New("could not read the Notion token")
	}
	err = notion.Login(context.Background(), string(secret))
	clear(secret)
	if err != nil {
		return err
	}
	fmt.Println("Notion connected. Token saved in macOS Keychain for future terminals and Studio sessions.")
	if os.Getenv("STAMP_NOTION_TOKEN") != "" {
		fmt.Println("STAMP_NOTION_TOKEN is set and overrides the saved token. Unset it to use Keychain.")
	}
	return nil
}

func logoutCommand(args []string) error {
	provider, err := authProvider(args, "logout")
	if err != nil {
		return err
	}
	if provider == "drive" {
		return stampdrive.Logout()
	}
	if err := notion.Logout(context.Background()); err != nil {
		return err
	}
	fmt.Println("Saved Notion token removed from Keychain. To revoke it, use Notion's token portal.")
	if os.Getenv("STAMP_NOTION_TOKEN") != "" {
		fmt.Println("STAMP_NOTION_TOKEN is still set; unset it to stop using that token in this terminal.")
	}
	return nil
}

func setupNotionProject(reader *bufio.Reader) error {
	fmt.Print("\nNext: [1] Clone a project  [2] Create a project  [3] Finish\nChoose [1]: ")
	choice, err := readLine(reader)
	if err != nil {
		return err
	}
	if choice == "3" {
		fmt.Println("Setup complete. Run stamp login notion whenever your token needs replacing.")
		return nil
	}
	if choice != "" && choice != "1" && choice != "2" {
		return errors.New("choose 1, 2, or 3")
	}
	fmt.Print("Local workspace folder [stamp-project]: ")
	dir, err := readLine(reader)
	if err != nil {
		return err
	}
	dir = defaultString(dir, "stamp-project")
	if choice == "2" {
		fmt.Print("Project name [My Project]: ")
		name, err := readLine(reader)
		if err != nil {
			return err
		}
		fmt.Print("Parent Notion page URL [leave blank for Private]: ")
		page, err := readLine(reader)
		if err != nil {
			return err
		}
		args := []string{dir, "--name", defaultString(name, "My Project"), "--backend", "notion"}
		if page != "" {
			args = append(args, "--notion-page", page)
		}
		if err := newCommand(args); err != nil {
			return err
		}
	} else {
		fmt.Print("Stamp project page URL (the root page): ")
		page, err := readLine(reader)
		if err != nil {
			return err
		}
		if err := cloneCommand([]string{dir, "--backend", "notion", "--notion-page", page}); err != nil {
			return err
		}
	}
	return offerStudio(reader, dir)
}
