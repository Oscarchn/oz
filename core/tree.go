package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// treeCardPageSize bounds how many directory/file entries are shown per card page.
const treeCardPageSize = 20

// errTreeEscape is returned when a requested path resolves outside the
// working-directory root. /tree is intentionally sandboxed to the workspace.
var errTreeEscape = errors.New("path escapes working directory")

// treeEntry is a single subdirectory or file inside the browsed directory.
type treeEntry struct {
	name  string
	isDir bool
	size  int64
}

// treeListing is the resolved, sandboxed contents of one directory level.
type treeListing struct {
	root     string // absolute workspace root
	cleanRel string // slash path relative to root ("" == root)
	dirs     []treeEntry
	files    []treeEntry
}

// cmdTree implements the /tree directory browser. With no argument it lists the
// working-directory root; with a relative subpath it lists that subdirectory.
// On card-capable platforms (e.g. Feishu) it renders an interactive card whose
// subdirectory buttons drill in place and whose file buttons open via /show.
func (e *Engine) cmdTree(p Platform, msg *Message, args []string) {
	agent, _, _, err := e.commandContext(p, msg)
	if err != nil {
		e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgWsResolutionError, err))
		return
	}
	if _, ok := agent.(WorkDirSwitcher); !ok {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgDirNotSupported))
		return
	}

	relPath := strings.TrimSpace(strings.Join(args, " "))
	switch strings.ToLower(relPath) {
	case "help", "-h", "--help":
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgTreeUsage))
		return
	}

	if supportsCards(p) {
		e.replyWithCard(p, msg.ReplyCtx, e.renderTreeCardSafe(msg.SessionKey, relPath, 1))
		return
	}

	// Plain-text fallback for platforms without rich cards.
	root := e.commandWorkDir(agent, msg)
	listing, errMsg := e.gatherTree(root, relPath)
	if errMsg != "" {
		e.reply(p, msg.ReplyCtx, errMsg)
		return
	}
	e.reply(p, msg.ReplyCtx, e.renderTreeText(listing))
}

// renderTreeCardSafe wraps renderTreeCard and returns an error card on failure.
func (e *Engine) renderTreeCardSafe(sessionKey, relPath string, page int) *Card {
	card, err := e.renderTreeCard(sessionKey, relPath, page)
	if err != nil {
		return e.simpleCard(e.i18n.T(MsgTreeCardTitle), "red", err.Error())
	}
	return card
}

// renderTreeCard resolves relPath under the session's working directory and
// builds the interactive directory-browser card for the given page.
func (e *Engine) renderTreeCard(sessionKey, relPath string, page int) (*Card, error) {
	agent, _ := e.sessionContextForKey(sessionKey)
	switcher, ok := agent.(WorkDirSwitcher)
	if !ok {
		return nil, fmt.Errorf("%s", e.i18n.T(MsgDirNotSupported))
	}
	root := normalizeWorkspacePath(switcher.GetWorkDir())
	listing, errMsg := e.gatherTree(root, relPath)
	if errMsg != "" {
		return nil, fmt.Errorf("%s", errMsg)
	}
	return e.buildTreeCard(listing, page), nil
}

// gatherTree resolves and sandboxes relPath under root, then lists its visible
// subdirectories and files. On failure it returns a localized error message.
func (e *Engine) gatherTree(root, relPath string) (*treeListing, string) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, e.i18n.T(MsgDirNotSupported)
	}
	abs, cleanRel, err := resolveTreeTarget(root, relPath)
	if err != nil {
		return nil, e.i18n.T(MsgTreeEscape)
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, e.i18n.Tf(MsgTreeNotFound, "/"+cleanRel)
		}
		return nil, e.i18n.Tf(MsgTreeReadFailed, err)
	}
	if !info.IsDir() {
		return nil, e.i18n.Tf(MsgTreeNotDir, "/"+cleanRel)
	}
	dirs, files, err := listTreeEntries(abs)
	if err != nil {
		return nil, e.i18n.Tf(MsgTreeReadFailed, err)
	}
	return &treeListing{root: root, cleanRel: cleanRel, dirs: dirs, files: files}, ""
}

// buildTreeCard renders a treeListing as an interactive card. Subdirectories
// become "Open" buttons (nav:/tree, in-place drill-down); files become "View"
// buttons (cmd:/show, dispatched as a new message).
func (e *Engine) buildTreeCard(listing *treeListing, page int) *Card {
	combined := make([]treeEntry, 0, len(listing.dirs)+len(listing.files))
	combined = append(combined, listing.dirs...)
	combined = append(combined, listing.files...)

	total := len(combined)
	totalPages := 1
	if total > 0 {
		totalPages = (total + treeCardPageSize - 1) / treeCardPageSize
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * treeCardPageSize
	end := start + treeCardPageSize
	if end > total {
		end = total
	}

	cb := NewCard().Title(e.i18n.T(MsgTreeCardTitle), "turquoise")
	cb.Markdown(e.i18n.Tf(MsgTreeCurrent, treeDisplayPath(listing.root, listing.cleanRel)))
	cb.Divider()

	if total == 0 {
		cb.Note(e.i18n.T(MsgTreeEmpty))
	} else {
		dirsHeaderShown, filesHeaderShown := false, false
		for i := start; i < end; i++ {
			ent := combined[i]
			if ent.isDir {
				if !dirsHeaderShown {
					cb.Markdown(e.i18n.T(MsgTreeDirsSection))
					dirsHeaderShown = true
				}
				childRel := joinTreeRel(listing.cleanRel, ent.name)
				cb.ListItemBtn(
					fmt.Sprintf("📁 `%s/`", ent.name),
					e.i18n.T(MsgTreeOpen),
					"primary",
					encodeTreeNav(childRel, 1),
				)
				continue
			}
			if !filesHeaderShown {
				cb.Markdown(e.i18n.T(MsgTreeFilesSection))
				filesHeaderShown = true
			}
			fileRel := joinTreeRel(listing.cleanRel, ent.name)
			cb.ListItemBtn(
				fmt.Sprintf("📄 `%s`  ·  %s", ent.name, formatTreeSize(ent.size)),
				e.i18n.T(MsgTreeView),
				"default",
				"cmd:/show ./"+fileRel,
			)
		}
	}

	// Navigation row: parent (when not at root) + back to the help menu.
	var navRow []CardButton
	if listing.cleanRel != "" {
		navRow = append(navRow, DefaultBtn(e.i18n.T(MsgTreeParent), encodeTreeNav(treeParentRel(listing.cleanRel), 1)))
	}
	navRow = append(navRow, e.cardBackButton())
	cb.Buttons(navRow...)

	// Pagination row + page hint (only when there is more than one page).
	if totalPages > 1 {
		var pageRow []CardButton
		if page > 1 {
			pageRow = append(pageRow, e.cardPrevButton(encodeTreeNav(listing.cleanRel, page-1)))
		}
		if page < totalPages {
			pageRow = append(pageRow, e.cardNextButton(encodeTreeNav(listing.cleanRel, page+1)))
		}
		cb.Buttons(pageRow...)
		cb.Note(e.i18n.Tf(MsgTreePageHint, page, totalPages))
	}

	cb.Note(e.i18n.Tf(MsgTreeCounts, len(listing.dirs), len(listing.files)))
	return cb.Build()
}

// renderTreeText renders a treeListing as plain text for platforms without
// rich-card support.
func (e *Engine) renderTreeText(listing *treeListing) string {
	var sb strings.Builder
	sb.WriteString(e.i18n.Tf(MsgTreeCurrent, treeDisplayPath(listing.root, listing.cleanRel)))
	sb.WriteString("\n")

	if len(listing.dirs) == 0 && len(listing.files) == 0 {
		sb.WriteString("\n")
		sb.WriteString(e.i18n.T(MsgTreeEmpty))
		return sb.String()
	}
	if len(listing.dirs) > 0 {
		sb.WriteString("\n")
		sb.WriteString(e.i18n.T(MsgTreeDirsSection))
		sb.WriteString("\n")
		for _, d := range listing.dirs {
			sb.WriteString(fmt.Sprintf("  📁 %s/\n", d.name))
		}
	}
	if len(listing.files) > 0 {
		sb.WriteString("\n")
		sb.WriteString(e.i18n.T(MsgTreeFilesSection))
		sb.WriteString("\n")
		for _, f := range listing.files {
			sb.WriteString(fmt.Sprintf("  📄 %s  ·  %s\n", f.name, formatTreeSize(f.size)))
		}
	}
	sb.WriteString("\n")
	sb.WriteString(e.i18n.Tf(MsgTreeCounts, len(listing.dirs), len(listing.files)))
	return sb.String()
}

// resolveTreeTarget joins relPath onto root and rejects anything that escapes
// the root (via "..", an absolute path, or a symlink pointing outside). It
// returns the absolute target path and the cleaned slash path relative to root.
func resolveTreeTarget(root, relPath string) (absPath, cleanRel string, err error) {
	relPath = strings.TrimSpace(relPath)
	abs := filepath.Clean(filepath.Join(root, filepath.FromSlash(relPath)))
	// Best-effort symlink resolution so links cannot tunnel out of the root.
	if resolved, e := filepath.EvalSymlinks(abs); e == nil {
		abs = resolved
	}
	rel, e := filepath.Rel(root, abs)
	if e != nil {
		return "", "", errTreeEscape
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", "", errTreeEscape
	}
	if rel == "." {
		rel = ""
	}
	return abs, rel, nil
}

// listTreeEntries reads absPath and splits its visible (non-hidden) children
// into sorted directory and file slices.
func listTreeEntries(absPath string) (dirs, files []treeEntry, err error) {
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, nil, err
	}
	for _, de := range entries {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue // skip hidden entries (dotfiles/dotdirs)
		}
		if de.IsDir() {
			dirs = append(dirs, treeEntry{name: name, isDir: true})
			continue
		}
		var size int64
		if info, e := de.Info(); e == nil {
			size = info.Size()
		}
		files = append(files, treeEntry{name: name, size: size})
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].name) < strings.ToLower(dirs[j].name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].name) < strings.ToLower(files[j].name) })
	return dirs, files, nil
}

// encodeTreeNav builds a nav action that re-renders the tree card at relPath/page.
// Format: "nav:/tree <page> [relPath]" — the page always precedes the (optional)
// relative path so decodeTreeNav can recover both even when relPath has spaces.
func encodeTreeNav(relPath string, page int) string {
	if relPath == "" {
		return fmt.Sprintf("nav:/tree %d", page)
	}
	return fmt.Sprintf("nav:/tree %d %s", page, relPath)
}

// decodeTreeNav parses the args portion of a "nav:/tree ..." action into a
// relative path and page number. A leading integer token is the page; anything
// after it is the path. Falls back to (args, 1) when there is no page token.
func decodeTreeNav(args string) (relPath string, page int) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "", 1
	}
	first, rest := args, ""
	if i := strings.IndexByte(args, ' '); i >= 0 {
		first = args[:i]
		rest = strings.TrimSpace(args[i+1:])
	}
	if n, err := strconv.Atoi(first); err == nil && n >= 1 {
		return rest, n
	}
	return args, 1
}

// joinTreeRel appends a child name onto a slash-form relative path.
func joinTreeRel(cleanRel, name string) string {
	if cleanRel == "" {
		return name
	}
	return cleanRel + "/" + name
}

// treeParentRel returns the parent of a slash-form relative path ("" at root).
func treeParentRel(cleanRel string) string {
	if i := strings.LastIndex(cleanRel, "/"); i >= 0 {
		return cleanRel[:i]
	}
	return ""
}

// treeDisplayPath renders a human-friendly breadcrumb: the workspace folder
// name followed by the relative path within it.
func treeDisplayPath(root, cleanRel string) string {
	base := filepath.Base(root)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = root
	}
	if cleanRel == "" {
		return base + "/"
	}
	return base + "/" + cleanRel
}

// formatTreeSize renders a byte count as a compact human-readable string.
func formatTreeSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
