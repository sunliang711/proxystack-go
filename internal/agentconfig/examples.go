package agentconfig

// StackExampleSnippet 描述一个可复制的 stack 配置片段。
type StackExampleSnippet struct {
	Area        string
	Section     string
	Type        string
	Description string
	Content     string
}

// StackExampleSnippets 返回 psctl example 支持的全部 stack 配置片段。
func StackExampleSnippets() []StackExampleSnippet {
	snippets, err := stackSnippetExamples()
	if err != nil {
		panic(err)
	}
	return snippets
}
