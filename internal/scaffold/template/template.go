package template

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/naoray/anvil/internal/scaffold/types"
)

func ReplaceTemplateVars(str string, ctx *types.ScaffoldContext) (string, error) {
	tmpl, err := template.New("").Option("missingkey=error").Parse(str)
	if err != nil {
		return "", fmt.Errorf("invalid template: %w", err)
	}

	data := ctx.SnapshotForTemplate()
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template execution failed: %w", err)
	}

	return buf.String(), nil
}
