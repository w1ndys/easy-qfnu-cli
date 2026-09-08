package qfnu

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type jwxtCommand struct {
	action      string
	ocrURL      string
	sessionPath string
	username    string
	password    string
	captcha     string
	output      string
	semester    string
	week        string
	mode        string
	targetScore int
	courses     []string
	save        bool
	saveSet     bool
	forget      bool
	confirm     bool
}

func runJWXT(args []string, out io.Writer) int {
	if len(args) == 0 || args[0] == "--help" {
		return printJWXTUsage(out)
	}
	if args[0] == "relay" {
		return runJWXTRelayCommand(args, out)
	}
	if args[0] == "xk" {
		return runJWXTXK(args[1:], out)
	}
	command, err := parseJWXTCommand(args[0], args[1:])
	if err != nil {
		return writeJSON(out, failure("jwxt", err.Error(), ""))
	}
	if command.action == "forget-credentials" {
		return runJWXTForgetCredentials(out)
	}
	client, err := prepareJWXTClient(command)
	if err != nil {
		return writeJSON(out, failure("jwxt", err.Error(), "请检查本地会话文件；可运行 logout 清理损坏会话"))
	}
	if command.action == "logout" {
		return runJWXTLogout(client, command.forget, out)
	}
	result, runErr := executeJWXTCommand(client, &command)
	return writeJWXTCommandResult(command.action, result, runErr, out)
}

func printJWXTUsage(out io.Writer) int {
	if _, err := fmt.Fprintln(out, "Usage: easy-qfnu jwxt <captcha|login|grades|schedule|evaluations|evaluate|status|logout|forget-credentials|relay|xk>"); err != nil {
		return 1
	}
	return 2
}

func parseJWXTCommand(action string, args []string) (jwxtCommand, error) {
	command := jwxtCommand{action: action, targetScore: 89}
	for index := 0; index < len(args); index++ {
		consumed, err := command.parseOption(args[index:])
		if err != nil {
			return jwxtCommand{}, err
		}
		index += consumed
	}
	command.applyEnvironment()
	return command, nil
}

func (c *jwxtCommand) parseOption(args []string) (int, error) {
	arg := args[0]
	switch arg {
	case "--confirm":
		c.confirm = true
		return 0, nil
	case "--forget-credentials", "--clear-credentials":
		c.forget = true
		return 0, nil
	case "--save-credentials":
		c.saveSet = true
		c.save = true
		if len(args) > 1 && (args[1] == "yes" || args[1] == "no") {
			c.save = args[1] == "yes"
			return 1, nil
		}
		return 0, nil
	}
	if len(args) < 2 {
		return 0, fmt.Errorf("%s requires a value", arg)
	}
	if err := c.setValueOption(arg, args[1]); err != nil {
		return 0, err
	}
	return 1, nil
}

func (c *jwxtCommand) setValueOption(arg, value string) error {
	switch arg {
	case "--ocr-url":
		c.ocrURL = value
	case "--session-path":
		c.sessionPath = value
	case "--username", "-u":
		c.username = value
	case "--password", "-p":
		c.password = value
	case "--captcha":
		c.captcha = value
	case "--out", "-o":
		c.output = value
	case "--semester", "--kksj", "--xnxq01id":
		c.semester = value
	case "--week", "--zc":
		c.week = value
	case "--kbjcmsid":
		c.mode = value
	case "--score", "--target-score":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s must be an integer", arg)
		}
		c.targetScore = parsed
	case "--course":
		c.courses = append(c.courses, strings.Split(value, ",")...)
	default:
		return fmt.Errorf("unknown option: %s", arg)
	}
	return nil
}

func (c *jwxtCommand) applyEnvironment() {
	if c.ocrURL == "" {
		c.ocrURL = os.Getenv("QFNU_OCR_URL")
	}
	if !c.saveSet && strings.EqualFold(os.Getenv("QFNU_JWXT_SAVE_CREDENTIALS"), "yes") {
		c.save = true
	}
}

func runJWXTRelayCommand(args []string, out io.Writer) int {
	if len(args) != 2 {
		return writeJSON(out, failure("jwxt", "relay requires one fixed action", "支持 feedback、recommendation、rank"))
	}
	client, err := newJWXTClient("", "")
	if err != nil {
		return writeJSON(out, failure("jwxt", err.Error(), "无法初始化教务客户端"))
	}
	if err := client.load(); err != nil {
		return writeJSON(out, failure("jwxt", err.Error(), "请检查本地会话文件"))
	}
	return runJWXTRelay(args[1], client, os.Stdin, out)
}

func runJWXTForgetCredentials(out io.Writer) int {
	removed, err := clearCredentialsFile()
	if err != nil {
		return writeJSON(out, failure("jwxt", err.Error(), "请检查凭据文件权限"))
	}
	return writeJSON(out, success("jwxt", payload{"credentials_removed": removed, "credentials_path": defaultCredentialsPath()}))
}

func prepareJWXTClient(command jwxtCommand) (*jwxtClient, error) {
	client, err := newJWXTClient(command.sessionPath, command.ocrURL)
	if err != nil {
		return nil, err
	}
	// Recovery actions replace or remove the old session, so a damaged session
	// file must not prevent captcha, password login, or logout.
	needsSession := command.action != "captcha" && command.action != "logout" && (command.action != "login" || command.captcha != "")
	if needsSession {
		if err := client.load(); err != nil {
			return nil, err
		}
	}
	return client, nil
}

func runJWXTLogout(client *jwxtClient, forget bool, out io.Writer) int {
	if err := client.clear(); err != nil {
		return writeJSON(out, failure("jwxt", err.Error(), "请检查本地会话文件权限"))
	}
	result := success("jwxt", payload{"logged_in": false, "session_path": client.sessionPath})
	if forget {
		removed, err := clearCredentialsFile()
		if err != nil {
			return writeJSON(out, failure("jwxt", err.Error(), "会话已清理；请检查凭据文件权限"))
		}
		result["credentials_removed"] = removed
		result["credentials_path"] = defaultCredentialsPath()
	}
	return writeJSON(out, result)
}

func executeJWXTCommand(client *jwxtClient, command *jwxtCommand) (payload, error) {
	switch command.action {
	case "captcha":
		if command.output == "" {
			command.output = defaultCaptchaPath()
		}
		return client.captcha(command.output)
	case "login":
		return loginJWXT(client, command)
	case "status", "whoami":
		return client.status()
	case "grades":
		return client.grades(command.semester)
	case "schedule":
		return client.schedule(command.semester, command.week, command.mode)
	case "evaluations":
		return client.evaluations()
	case "evaluate":
		return client.evaluate(command.targetScore, command.courses, command.confirm)
	default:
		return nil, fmt.Errorf("unknown action: %s", command.action)
	}
}

func loginJWXT(client *jwxtClient, command *jwxtCommand) (payload, error) {
	if command.username == "" {
		command.username = os.Getenv("QFNU_JWXT_USERNAME")
	}
	if command.password == "" {
		command.password = os.Getenv("QFNU_JWXT_PASSWORD")
	}
	if command.username == "" || command.password == "" {
		username, password, err := loadCredentialsFile()
		if err != nil {
			return nil, err
		}
		command.username = username
		command.password = password
	}
	return client.login(command.username, command.password, command.captcha, command.save)
}

// reportableJWXTActions 是需要匿名统计的在线功能；captcha/status/logout 等
// 本地或辅助操作不上报。login 的成功事件在 reportLoginSuccess 中单独处理，
// 失败不上报（issue #2 语义：登录失败不发送登录成功事件，也不上报失败）。
func reportableJWXTActions(action string) bool {
	switch action {
	case "grades", "schedule", "evaluations", "evaluate":
		return true
	}
	return false
}

func writeJWXTCommandResult(action string, result payload, err error, out io.Writer) int {
	if err != nil {
		if reportableJWXTActions(action) {
			reportUsage("jwxt."+action, "failure")
		}
		if known, ok := err.(*jwxtError); ok {
			result = failure("jwxt", known.message, known.hint)
		} else {
			result = failure("jwxt", err.Error(), "请检查网络和本地会话后重试")
		}
		return writeJSON(out, result)
	}
	if action == "login" {
		reportLoginSuccess(result)
	} else if reportableJWXTActions(action) {
		reportUsage("jwxt."+action, "success")
	}
	return writeJSON(out, result)
}
