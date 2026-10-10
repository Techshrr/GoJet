package support

import "testing"

func TestAdminTemplateValidation(t *testing.T) {
	base := AdminMailTemplateView{Key: "example", Locale: "en", Version: 2, VariableAllowlist: []string{"display_name"}}
	patch := AdminMailTemplatePatch{Locale: "en", ExpectedVersion: 2, Subject: "Hello {{display_name}}", Text: "Hello {{display_name}}", HTML: "<p>{{display_name}}</p>"}
	rendered, err := renderAdminTemplate(base, patch, map[string]string{"display_name": "<script>"})
	if err != nil || rendered.HTML != "<p>&lt;script&gt;</p>" {
		t.Fatalf("escaped preview: %+v %v", rendered, err)
	}
	for name, change := range map[string]func(*AdminMailTemplatePatch){
		"stale-version":    func(p *AdminMailTemplatePatch) { p.ExpectedVersion = 1 },
		"wrong-locale":     func(p *AdminMailTemplatePatch) { p.Locale = "zh-CN" },
		"header-injection": func(p *AdminMailTemplatePatch) { p.Subject = "Hello\r\nBcc: other@example.test" },
		"unknown-variable": func(p *AdminMailTemplatePatch) { p.Text = "{{smtp_password}}" },
		"empty-body":       func(p *AdminMailTemplatePatch) { p.Text = ""; p.HTML = "" },
	} {
		t.Run(name, func(t *testing.T) {
			p := patch
			change(&p)
			if _, err := renderAdminTemplate(base, p, templateSamples(base)); err == nil {
				t.Fatal("invalid template accepted")
			}
		})
	}
}
