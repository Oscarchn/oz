package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTreeTestEngine(root string, p Platform) *Engine {
	return NewEngine("test", &stubWorkDirAgent{workDir: root}, []Platform{p}, "", LangEnglish)
}

// treeListItems extracts all CardListItem elements from a card.
func treeListItems(c *Card) []CardListItem {
	var out []CardListItem
	for _, el := range c.Elements {
		if li, ok := el.(CardListItem); ok {
			out = append(out, li)
		}
	}
	return out
}

// treeButtons extracts all buttons across every CardActions row in a card.
func treeButtons(c *Card) []CardButton {
	var out []CardButton
	for _, el := range c.Elements {
		if a, ok := el.(CardActions); ok {
			out = append(out, a.Buttons...)
		}
	}
	return out
}

func hasButtonText(c *Card, text string) bool {
	for _, b := range treeButtons(c) {
		if b.Text == text {
			return true
		}
	}
	return false
}

func TestResolveTreeTarget(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	tests := []struct {
		name    string
		relPath string
		wantRel string
		wantErr bool
	}{
		{"root empty", "", "", false},
		{"root dot", ".", "", false},
		{"single", "sub", "sub", false},
		{"nested", "a/b", "a/b", false},
		{"cleaned", "a/../b", "b", false},
		{"trailing slash", "sub/", "sub", false},
		{"absolute is sandboxed", "/etc", "etc", false},
		{"parent escape", "..", "", true},
		{"deep escape", "../../x", "", true},
		{"sneaky escape", "a/../../x", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, rel, err := resolveTreeTarget(root, tt.relPath)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected escape error, got rel=%q", rel)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rel != tt.wantRel {
				t.Fatalf("cleanRel = %q, want %q", rel, tt.wantRel)
			}
		})
	}
}

func TestDecodeTreeNav(t *testing.T) {
	tests := []struct {
		in       string
		wantRel  string
		wantPage int
	}{
		{"", "", 1},
		{"2", "", 2},
		{"1 src", "src", 1},
		{"3 a/b c", "a/b c", 3}, // path containing a space is preserved
		{"src", "src", 1},       // non-numeric first token → treated as a path
		{"  2   src  ", "src", 2},
		{"10 core/agent/claudecode", "core/agent/claudecode", 10},
	}
	for _, tt := range tests {
		rel, page := decodeTreeNav(tt.in)
		if rel != tt.wantRel || page != tt.wantPage {
			t.Errorf("decodeTreeNav(%q) = (%q, %d), want (%q, %d)", tt.in, rel, page, tt.wantRel, tt.wantPage)
		}
	}
}

func TestEncodeTreeNavRoundTrip(t *testing.T) {
	cases := []struct {
		rel  string
		page int
	}{
		{"", 1},
		{"", 4},
		{"src", 1},
		{"a/b c", 3},
	}
	for _, c := range cases {
		action := encodeTreeNav(c.rel, c.page)
		// Mirror handleCardNav: strip "nav:/tree " then decode the remainder.
		body := strings.TrimPrefix(action, "nav:")
		args := strings.TrimPrefix(body, "/tree")
		args = strings.TrimSpace(args)
		gotRel, gotPage := decodeTreeNav(args)
		if gotRel != c.rel || gotPage != c.page {
			t.Errorf("round trip %q/%d via %q = (%q, %d)", c.rel, c.page, action, gotRel, gotPage)
		}
	}
}

func TestListTreeEntries(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "beta"))
	mustMkdir(t, filepath.Join(root, "alpha"))
	mustMkdir(t, filepath.Join(root, ".git")) // hidden dir → skipped
	mustWrite(t, filepath.Join(root, "b.go"), "package main")
	mustWrite(t, filepath.Join(root, "a.txt"), "hello")
	mustWrite(t, filepath.Join(root, ".env"), "SECRET=1") // hidden file → skipped

	dirs, files, err := listTreeEntries(root)
	if err != nil {
		t.Fatalf("listTreeEntries: %v", err)
	}
	if got := dirNames(dirs); !equalStrings(got, []string{"alpha", "beta"}) {
		t.Fatalf("dirs = %v, want [alpha beta] (hidden skipped, sorted)", got)
	}
	if got := dirNames(files); !equalStrings(got, []string{"a.txt", "b.go"}) {
		t.Fatalf("files = %v, want [a.txt b.go] (hidden skipped, sorted)", got)
	}
	for _, f := range files {
		if f.name == "a.txt" && f.size != int64(len("hello")) {
			t.Errorf("a.txt size = %d, want %d", f.size, len("hello"))
		}
	}
}

func TestListTreeEntriesCaseInsensitiveSort(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "Zeta"))
	mustMkdir(t, filepath.Join(root, "alpha"))
	dirs, _, err := listTreeEntries(root)
	if err != nil {
		t.Fatalf("listTreeEntries: %v", err)
	}
	if got := dirNames(dirs); !equalStrings(got, []string{"alpha", "Zeta"}) {
		t.Fatalf("dirs = %v, want [alpha Zeta] (case-insensitive sort)", got)
	}
}

func TestFormatTreeSize(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tt := range tests {
		if got := formatTreeSize(tt.n); got != tt.want {
			t.Errorf("formatTreeSize(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestRenderTreeCardRoot(t *testing.T) {
	root := mustEvalTempDir(t)
	mustMkdir(t, filepath.Join(root, "core"))
	mustWrite(t, filepath.Join(root, "readme.md"), "docs")

	e := newTreeTestEngine(root, &stubCardPlatform{stubPlatformEngine: stubPlatformEngine{n: "feishu"}})
	card, err := e.renderTreeCard("s1", "", 1)
	if err != nil {
		t.Fatalf("renderTreeCard: %v", err)
	}
	if card.Header == nil || card.Header.Title != e.i18n.T(MsgTreeCardTitle) {
		t.Fatalf("unexpected header: %+v", card.Header)
	}

	items := treeListItems(card)
	var dirItem, fileItem *CardListItem
	for i := range items {
		switch {
		case strings.Contains(items[i].Text, "core"):
			dirItem = &items[i]
		case strings.Contains(items[i].Text, "readme.md"):
			fileItem = &items[i]
		}
	}
	if dirItem == nil {
		t.Fatal("no list item for subdirectory 'core'")
	}
	if dirItem.BtnValue != "nav:/tree 1 core" {
		t.Errorf("dir button value = %q, want %q", dirItem.BtnValue, "nav:/tree 1 core")
	}
	if fileItem == nil {
		t.Fatal("no list item for file 'readme.md'")
	}
	if fileItem.BtnValue != "cmd:/show ./readme.md" {
		t.Errorf("file button value = %q, want %q", fileItem.BtnValue, "cmd:/show ./readme.md")
	}

	// At the root there must be no parent ("up") button.
	if hasButtonText(card, e.i18n.T(MsgTreeParent)) {
		t.Error("root card should not have a parent button")
	}
}

func TestRenderTreeCardSubdirHasParent(t *testing.T) {
	root := mustEvalTempDir(t)
	mustMkdir(t, filepath.Join(root, "core", "agent"))

	e := newTreeTestEngine(root, &stubCardPlatform{stubPlatformEngine: stubPlatformEngine{n: "feishu"}})
	card, err := e.renderTreeCard("s1", "core", 1)
	if err != nil {
		t.Fatalf("renderTreeCard: %v", err)
	}
	if !hasButtonText(card, e.i18n.T(MsgTreeParent)) {
		t.Fatal("subdir card should have a parent button")
	}
	// Parent of "core" is the root → nav:/tree 1.
	var parentVal string
	for _, b := range treeButtons(card) {
		if b.Text == e.i18n.T(MsgTreeParent) {
			parentVal = b.Value
		}
	}
	if parentVal != "nav:/tree 1" {
		t.Errorf("parent button value = %q, want %q", parentVal, "nav:/tree 1")
	}
	// Drilling into the nested "agent" dir carries the full relative path.
	var childVal string
	for _, li := range treeListItems(card) {
		if strings.Contains(li.Text, "agent") {
			childVal = li.BtnValue
		}
	}
	if childVal != "nav:/tree 1 core/agent" {
		t.Errorf("child dir value = %q, want %q", childVal, "nav:/tree 1 core/agent")
	}
}

func TestRenderTreeCardRejectsEscape(t *testing.T) {
	root := mustEvalTempDir(t)
	e := newTreeTestEngine(root, &stubCardPlatform{stubPlatformEngine: stubPlatformEngine{n: "feishu"}})

	if _, err := e.renderTreeCard("s1", "..", 1); err == nil {
		t.Fatal("expected error for path escaping the working directory")
	}
	// renderTreeCardSafe degrades to a red error card instead of panicking.
	card := e.renderTreeCardSafe("s1", "../../etc", 1)
	if card == nil || card.Header == nil || card.Header.Color != "red" {
		t.Fatalf("expected red error card, got %+v", card)
	}
}

func TestRenderTreeCardRejectsFile(t *testing.T) {
	root := mustEvalTempDir(t)
	mustWrite(t, filepath.Join(root, "main.go"), "package main")
	e := newTreeTestEngine(root, &stubCardPlatform{stubPlatformEngine: stubPlatformEngine{n: "feishu"}})

	if _, err := e.renderTreeCard("s1", "main.go", 1); err == nil {
		t.Fatal("expected error when target is a file, not a directory")
	}
}

func TestRenderTreeCardPagination(t *testing.T) {
	root := mustEvalTempDir(t)
	for i := 0; i < 25; i++ {
		mustMkdir(t, filepath.Join(root, fmt.Sprintf("d%02d", i)))
	}
	e := newTreeTestEngine(root, &stubCardPlatform{stubPlatformEngine: stubPlatformEngine{n: "feishu"}})

	page1, err := e.renderTreeCard("s1", "", 1)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if n := len(treeListItems(page1)); n != treeCardPageSize {
		t.Errorf("page1 items = %d, want %d", n, treeCardPageSize)
	}
	if !hasButtonText(page1, e.i18n.T(MsgCardNext)) {
		t.Error("page1 should have a Next button")
	}
	if hasButtonText(page1, e.i18n.T(MsgCardPrev)) {
		t.Error("page1 should not have a Prev button")
	}

	page2, err := e.renderTreeCard("s1", "", 2)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if n := len(treeListItems(page2)); n != 5 {
		t.Errorf("page2 items = %d, want 5", n)
	}
	if !hasButtonText(page2, e.i18n.T(MsgCardPrev)) {
		t.Error("page2 should have a Prev button")
	}
}

func TestCmdTreeSendsCardOnCardPlatform(t *testing.T) {
	root := mustEvalTempDir(t)
	mustMkdir(t, filepath.Join(root, "core"))
	p := &stubCardPlatform{stubPlatformEngine: stubPlatformEngine{n: "feishu"}}
	e := newTreeTestEngine(root, p)

	e.cmdTree(p, &Message{SessionKey: "s1"}, nil)

	p.mu.Lock()
	n := len(p.repliedCards)
	p.mu.Unlock()
	if n != 1 {
		t.Fatalf("expected 1 card reply, got %d", n)
	}
}

func TestCmdTreeTextFallback(t *testing.T) {
	root := mustEvalTempDir(t)
	mustMkdir(t, filepath.Join(root, "core"))
	mustWrite(t, filepath.Join(root, "go.mod"), "module x")
	p := &stubPlatformEngine{n: "telegram"} // no CardSender
	e := newTreeTestEngine(root, p)

	e.cmdTree(p, &Message{SessionKey: "s1"}, nil)

	sent := p.getSent()
	if len(sent) != 1 {
		t.Fatalf("expected 1 text reply, got %d: %v", len(sent), sent)
	}
	body := sent[0]
	if !strings.Contains(body, "core") || !strings.Contains(body, "go.mod") {
		t.Errorf("text reply missing entries: %q", body)
	}
}

func TestCmdTreeHelp(t *testing.T) {
	root := mustEvalTempDir(t)
	p := &stubPlatformEngine{n: "telegram"}
	e := newTreeTestEngine(root, p)

	e.cmdTree(p, &Message{SessionKey: "s1"}, []string{"help"})

	sent := p.getSent()
	if len(sent) != 1 || sent[0] != e.i18n.T(MsgTreeUsage) {
		t.Fatalf("expected usage text, got %v", sent)
	}
}

// --- helpers ---

func mustEvalTempDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	return root
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func dirNames(entries []treeEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.name
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
