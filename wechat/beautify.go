package wechat

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/ditashi/jsbeautifier-go/jsbeautifier"
	"github.com/dop251/goja/parser"
	"github.com/tidwall/pretty"
	"github.com/yosssi/gohtml"
)

var regScriptInHtml = regexp.MustCompile(`(?s) *<script.*?>(.*?)</script>`)
var jsOptions = jsbeautifier.DefaultOptions()

func PrettyJson(data []byte) []byte {
	return pretty.Pretty(data)
}

func PrettyHtml(data []byte) []byte {
	data = gohtml.FormatBytes(bytes.TrimSpace(data)) // use `TrimSpace` to remove leading whitespace
	data = regScriptInHtml.ReplaceAllFunc(data, func(script []byte) []byte {
		var space = countLeadingSpaces(script)

		var jsCode = regScriptInHtml.FindSubmatch(script)[1]
		var jsStr = strings.Repeat(" ", space+2) + string(bytes.TrimSpace(jsCode))

		beautified, ok := beautifyJavaScript(jsStr)
		if ok {
			return bytes.Replace(script, jsCode, []byte("\n"+beautified+"\n"+strings.Repeat(" ", space)), 1)
		}
		return script
	})
	return data
}

func PrettyJavaScript(data []byte) []byte {
	var code = string(bytes.TrimSpace(data))
	beautified, ok := beautifyJavaScript(code)
	if !ok {
		return data
	}

	return []byte(beautified)
}

func beautifyJavaScript(code string) (string, bool) {
	beautified, err := jsbeautifier.Beautify(&code, jsOptions)
	if err != nil {
		return code, false
	}
	if strings.TrimSpace(beautified) == "" {
		if strings.TrimSpace(code) == "" {
			return beautified, true
		}
		return code, false
	}
	if _, err := parser.ParseFile(nil, "beautified.js", beautified, 0); err != nil {
		return code, false
	}
	return beautified, true
}

func countLeadingSpaces(data []byte) int {
	var result = 0
	for _, c := range data {
		if c == ' ' {
			result++
		} else {
			return result
		}
	}

	return result
}
