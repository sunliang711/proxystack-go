package sub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	builtintemplates "github.com/eagle/proxystack-go/templates"
	"github.com/flosch/pongo2/v6"
	"gopkg.in/yaml.v3"
)

var (
	registerFiltersOnce sync.Once
	registerFiltersErr  error
	templateTokenRe     = regexp.MustCompile(`(?s)(\{\{.*?\}\}|\{%.*?%\})`)
	templateForTagRe    = regexp.MustCompile(`^for\s+([A-Za-z_][A-Za-z0-9_]*)\s+in\s+(.+)$`)
	identifierPathRe    = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*`)
	stringLiteralRe     = regexp.MustCompile(`"[^"\\]*(?:\\.[^"\\]*)*"|'[^'\\]*(?:\\.[^'\\]*)*'`)
)

// RenderTemplate 按覆盖目录、data_dir 和内置模板顺序渲染订阅模板。
func RenderTemplate(templateName string, context map[string]any, templateDir string, dataDir string) (string, error) {
	source, err := LoadTemplate(templateName, templateDir, dataDir)
	if err != nil {
		return "", err
	}
	if err := validateTemplateVariables(source, context); err != nil {
		return "", err
	}
	if err := registerTemplateFilters(); err != nil {
		return "", err
	}
	template, err := pongo2.FromString(source)
	if err != nil {
		return "", TemplateError{Message: fmt.Sprintf("subscription template parse failed: %s", templateName)}
	}
	rendered, err := template.Execute(pongo2.Context(context))
	if err != nil {
		return "", TemplateError{Message: fmt.Sprintf("subscription template render failed: %s", templateName)}
	}
	return rendered, nil
}

// LoadTemplate 按约定顺序读取模板文本。
func LoadTemplate(templateName string, templateDir string, dataDir string) (string, error) {
	if filepath.Base(templateName) != templateName {
		return "", TemplateError{Message: "unsafe subscription template name: " + templateName}
	}
	for _, candidate := range TemplateCandidates(templateName, templateDir, dataDir) {
		data, err := os.ReadFile(candidate)
		if err == nil {
			return string(data), nil
		}
		if !os.IsNotExist(err) {
			return "", TemplateError{Message: fmt.Sprintf("subscription template could not be read: %s", candidate)}
		}
	}
	data, err := builtintemplates.SubFS.ReadFile(filepath.ToSlash(filepath.Join("sub", templateName)))
	if err != nil {
		return "", TemplateError{Message: "subscription template is missing: " + templateName}
	}
	return string(data), nil
}

// TemplateCandidates 返回本地覆盖模板候选路径。
func TemplateCandidates(templateName string, templateDir string, dataDir string) []string {
	candidates := make([]string, 0, 3)
	if templateDir != "" {
		candidates = append(candidates, filepath.Join(templateDir, "sub", templateName))
		candidates = append(candidates, filepath.Join(templateDir, templateName))
	}
	if dataDir != "" {
		candidates = append(candidates, filepath.Join(dataDir, "templates", "sub", templateName))
	}
	return uniqueStrings(candidates)
}

// TemplateSource 返回模板来源摘要，供启动日志使用。
func TemplateSource(templateName string, templateDir string, dataDir string) (string, error) {
	if filepath.Base(templateName) != templateName {
		return "", TemplateError{Message: "unsafe subscription template name: " + templateName}
	}
	for _, candidate := range TemplateCandidates(templateName, templateDir, dataDir) {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", TemplateError{Message: fmt.Sprintf("subscription template could not be read: %s", candidate)}
		}
	}
	return "builtin:sub/" + templateName, nil
}

func registerTemplateFilters() error {
	registerFiltersOnce.Do(func() {
		if err := pongo2.RegisterFilter("yaml_block", yamlBlockFilter); err != nil && !strings.Contains(err.Error(), "already registered") {
			registerFiltersErr = err
			return
		}
		if err := pongo2.RegisterFilter("tojson", toJSONFilter); err != nil && !strings.Contains(err.Error(), "already registered") {
			registerFiltersErr = err
			return
		}
	})
	return registerFiltersErr
}

func yamlBlockFilter(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(in.Interface()); err != nil {
		return nil, &pongo2.Error{Sender: "filter:yaml_block", OrigError: err}
	}
	if err := encoder.Close(); err != nil {
		return nil, &pongo2.Error{Sender: "filter:yaml_block", OrigError: err}
	}
	text := strings.TrimRight(buffer.String(), "\n")
	indent := 2
	if param != nil && param.IsInteger() {
		indent = param.Integer()
	}
	if indent > 0 {
		prefix := strings.Repeat(" ", indent)
		lines := strings.Split(text, "\n")
		for index, line := range lines {
			if line != "" {
				lines[index] = prefix + line
			}
		}
		text = strings.Join(lines, "\n")
	}
	return pongo2.AsSafeValue(text), nil
}

func toJSONFilter(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	text, err := toJSONString(in.Interface())
	if err != nil {
		return nil, &pongo2.Error{Sender: "filter:tojson", OrigError: err}
	}
	return pongo2.AsSafeValue(text), nil
}

func validateTemplateVariables(source string, context map[string]any) error {
	rootScope := map[string]bool{}
	for key := range context {
		rootScope[key] = true
	}
	scopes := []map[string]bool{rootScope}
	for _, token := range templateTokenRe.FindAllString(source, -1) {
		switch {
		case strings.HasPrefix(token, "{{"):
			if err := validateTemplateExpression(trimTemplateToken(token, "{{", "}}"), scopes); err != nil {
				return err
			}
		case strings.HasPrefix(token, "{%"):
			tag := trimTemplateToken(token, "{%", "%}")
			switch {
			case strings.HasPrefix(tag, "for "):
				match := templateForTagRe.FindStringSubmatch(tag)
				if match == nil {
					return TemplateError{Message: "subscription template contains unsupported for tag"}
				}
				if err := validateTemplateExpression(match[2], scopes); err != nil {
					return err
				}
				scopes = append(scopes, map[string]bool{match[1]: true})
			case tag == "endfor":
				if len(scopes) == 1 {
					return TemplateError{Message: "subscription template contains unexpected endfor"}
				}
				scopes = scopes[:len(scopes)-1]
			case strings.HasPrefix(tag, "if "):
				if err := validateTemplateExpression(strings.TrimSpace(strings.TrimPrefix(tag, "if ")), scopes); err != nil {
					return err
				}
			case strings.HasPrefix(tag, "elif "):
				if err := validateTemplateExpression(strings.TrimSpace(strings.TrimPrefix(tag, "elif ")), scopes); err != nil {
					return err
				}
			}
		}
	}
	if len(scopes) != 1 {
		return TemplateError{Message: "subscription template contains unclosed for tag"}
	}
	return nil
}

func validateTemplateExpression(expression string, scopes []map[string]bool) error {
	for _, root := range rootIdentifiers(expression) {
		if !templateVariableAvailable(root, scopes) {
			return TemplateError{Message: "subscription template references undefined variable: " + root}
		}
	}
	return nil
}

func rootIdentifiers(expression string) []string {
	segments := strings.Split(expression, "|")
	expression = stringLiteralRe.ReplaceAllString(segments[0], " ")
	seen := map[string]bool{}
	roots := make([]string, 0)
	for _, match := range identifierPathRe.FindAllString(expression, -1) {
		root := strings.SplitN(match, ".", 2)[0]
		if root == "" || templateKeyword(root) || seen[root] {
			continue
		}
		seen[root] = true
		roots = append(roots, root)
	}
	return roots
}

func templateVariableAvailable(name string, scopes []map[string]bool) bool {
	for index := len(scopes) - 1; index >= 0; index-- {
		if scopes[index][name] {
			return true
		}
	}
	return false
}

func templateKeyword(value string) bool {
	switch value {
	case "and", "or", "not", "in", "is", "true", "false", "none", "nil":
		return true
	default:
		return false
	}
}

func trimTemplateToken(token string, open string, close string) string {
	value := strings.TrimPrefix(strings.TrimSuffix(token, close), open)
	return strings.Trim(value, " \t\r\n-")
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	return unique
}

func jsonMarshalString(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
