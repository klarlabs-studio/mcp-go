package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Skills extension (SEP-2640, experimental): serve Agent Skills as ordinary
// MCP resources under skill://, with an optional skill://index.json catalog.
// No new RPC methods — resources/list and resources/read are the wire surface.
//
// EXPERIMENTAL: SEP-2640 is still a Draft Extensions Track SEP. The API and
// wire shape may change as the upstream standard settles. Do not rely on
// stability across minor releases until the SEP reaches Final.

const (
	// SkillScheme is the conventional URI scheme for MCP-served skills.
	SkillScheme = "skill"
	// SkillEntryPoint is the required skill document at each skill root.
	SkillEntryPoint = "SKILL.md"
	// SkillIndexURI is the well-known catalog resource (SEP-2640).
	SkillIndexURI = "skill://index.json"
	// SkillIndexSchema is the Agent Skills discovery index schema URI.
	SkillIndexSchema = "https://schemas.agentskills.io/discovery/0.2.0/schema.json"
	// SkillIndexMIME is the mimeType for skill://index.json.
	SkillIndexMIME = "application/json"
	// SkillMarkdownMIME is the recommended mimeType for SKILL.md.
	SkillMarkdownMIME = "text/markdown"
	// SkillArchiveMIMEGzip is the mimeType for .tar.gz skill archives (SEP-2640).
	SkillArchiveMIMEGzip = "application/gzip"
	// SkillArchiveMIMEZip is the mimeType for .zip skill archives (SEP-2640).
	SkillArchiveMIMEZip = "application/zip"

	// SkillIndex entry types.
	SkillIndexTypeSkillMD  = "skill-md"
	SkillIndexTypeArchive  = "archive"
	SkillIndexTypeTemplate = "mcp-resource-template"

	frontmatterKeyName        = "name"
	frontmatterKeyDescription = "description"
	mimeTextPlain             = "text/plain"
	mimeImagePNG              = "image/png"
	mimeImageSVG              = "image/svg+xml"
)

// SkillIndex is the JSON body of skill://index.json.
type SkillIndex struct {
	Schema string            `json:"$schema"`
	Skills []SkillIndexEntry `json:"skills"`
}

// SkillIndexEntry describes one cataloged skill (or template space).
type SkillIndexEntry struct {
	Name        string `json:"name,omitempty"`
	Type        string `json:"type"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

// SkillFrontmatter is the subset of Agent Skills YAML frontmatter used for
// resource metadata and the index.
type SkillFrontmatter struct {
	Name        string
	Description string
}

type skillRegistry struct {
	entries []SkillIndexEntry
	indexed bool
}

// Skill starts registering a skill at the given skill path (the URI path under
// skill://, e.g. "git-workflow" or "acme/billing/refunds"). Call FromDir to
// load files from disk. The final path segment MUST match the SKILL.md
// frontmatter name.
//
// Experimental: SEP-2640 is still in definition; see the package comment.
func (s *Server) Skill(skillPath string) *SkillBuilder {
	return &SkillBuilder{server: s, skillPath: strings.Trim(skillPath, "/")}
}

// SkillBuilder registers one skill's files as skill:// resources.
type SkillBuilder struct {
	server    *Server
	skillPath string
}

// FromDir walks dir, registers every file as a skill:// resource, and adds a
// skill-md entry to skill://index.json. dir must contain SKILL.md.
func (b *SkillBuilder) FromDir(dir string) error {
	if b == nil || b.server == nil {
		return fmt.Errorf("skill: nil builder")
	}
	if b.skillPath == "" {
		return fmt.Errorf("skill: empty skill path")
	}
	if err := validateSkillPath(b.skillPath); err != nil {
		return err
	}
	root, fm, err := b.loadSkillRoot(dir)
	if err != nil {
		return err
	}
	files, err := listSkillFiles(root, b.skillPath)
	if err != nil {
		return err
	}
	for _, rel := range files {
		b.registerSkillFile(root, rel, fm)
	}
	b.server.addSkillIndexEntry(SkillIndexEntry{
		Name:        fm.Name,
		Type:        SkillIndexTypeSkillMD,
		Description: fm.Description,
		URL:         skillFileURI(b.skillPath, SkillEntryPoint),
	})
	return b.server.Err()
}

// FromArchive registers a packed skill (.tar.gz or .zip) as a single skill://
// resource and adds a type:"archive" entry to skill://index.json.
//
// The archive URI is skill://{skillPath}{suffix} (e.g. skill://pdf.tar.gz).
// Hosts unpack into skill://{skillPath}/… per SEP-2640. The archive MUST
// contain SKILL.md at its root; this method only validates the path, suffix,
// and frontmatter name when a sibling SKILL.md is not required on disk —
// callers are responsible for archive contents.
//
// Experimental: SEP-2640 is still in definition; see the package comment.
func (b *SkillBuilder) FromArchive(archivePath string) error {
	if b == nil || b.server == nil {
		return fmt.Errorf("skill: nil builder")
	}
	if b.skillPath == "" {
		return fmt.Errorf("skill: empty skill path")
	}
	if err := validateSkillPath(b.skillPath); err != nil {
		return err
	}
	abs, err := filepath.Abs(archivePath)
	if err != nil {
		return fmt.Errorf("skill %q: resolve archive: %w", b.skillPath, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("skill %q: %w", b.skillPath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("skill %q: archive path %q is a directory", b.skillPath, abs)
	}
	suffix, mime, err := skillArchiveSuffix(abs)
	if err != nil {
		return fmt.Errorf("skill %q: %w", b.skillPath, err)
	}
	uri := SkillScheme + "://" + b.skillPath + suffix
	final := filepath.Base(b.skillPath)
	desc := "Archived skill " + final
	// Best-effort: if a sibling SKILL.md exists next to the archive, use its
	// frontmatter description (common layout: skill-dir/SKILL.md + skill-dir.tar.gz).
	if sibling := filepath.Join(filepath.Dir(abs), SkillEntryPoint); fileExists(sibling) {
		raw, readErr := os.ReadFile(sibling) //nolint:gosec // sibling path is under the caller-supplied archive dir.
		if readErr == nil {
			if fm, fmErr := parseSkillFrontmatter(string(raw)); fmErr == nil {
				if fm.Name != final {
					return fmt.Errorf("skill %q: frontmatter name %q must equal final path segment %q", b.skillPath, fm.Name, final)
				}
				desc = fm.Description
			}
		}
	}
	b.server.Resource(uri).
		Name(final).
		Description(desc).
		MimeType(mime).
		Handler(func(_ context.Context, reqURI string, _ map[string]string) (*ResourceContent, error) {
			data, err := os.ReadFile(abs) //nolint:gosec // abs came from filepath.Abs of caller path.
			if err != nil {
				return nil, err
			}
			return &ResourceContent{
				URI:      reqURI,
				MimeType: mime,
				Blob:     base64.StdEncoding.EncodeToString(data),
			}, nil
		})
	b.server.addSkillIndexEntry(SkillIndexEntry{
		Name:        final,
		Type:        SkillIndexTypeArchive,
		Description: desc,
		URL:         uri,
	})
	return b.server.Err()
}

func skillArchiveSuffix(path string) (suffix, mime string, err error) {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return ".tar.gz", SkillArchiveMIMEGzip, nil
	case strings.HasSuffix(lower, ".tgz"):
		return ".tar.gz", SkillArchiveMIMEGzip, nil
	case strings.HasSuffix(lower, ".zip"):
		return ".zip", SkillArchiveMIMEZip, nil
	default:
		return "", "", fmt.Errorf("archive must be .tar.gz, .tgz, or .zip")
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// SkillTemplate registers a parameterized skill namespace as an MCP resource
// template and a type:"mcp-resource-template" index entry (no name).
// uriTemplate must be an RFC 6570-style skill:// template that resolves to a
// SKILL.md URI, e.g. "skill://docs/{product}/SKILL.md". Chain Handler to serve
// concrete URIs after variable binding.
//
// Experimental: SEP-2640 is still in definition; see the package comment.
func (s *Server) SkillTemplate(uriTemplate, description string) *ResourceBuilder {
	if s == nil {
		return &ResourceBuilder{err: fmt.Errorf("skill template: nil server")}
	}
	uriTemplate = strings.TrimSpace(uriTemplate)
	description = strings.TrimSpace(description)
	if uriTemplate == "" {
		return &ResourceBuilder{server: s, err: fmt.Errorf("skill template: empty uri template")}
	}
	if description == "" {
		return &ResourceBuilder{server: s, err: fmt.Errorf("skill template: empty description")}
	}
	if !strings.HasPrefix(uriTemplate, SkillScheme+"://") {
		return &ResourceBuilder{server: s, err: fmt.Errorf("skill template: uri must start with %s://", SkillScheme)}
	}
	if !strings.Contains(uriTemplate, "{") {
		return &ResourceBuilder{server: s, err: fmt.Errorf("skill template: uri must include at least one {variable}")}
	}
	s.addSkillIndexEntry(SkillIndexEntry{
		Type:        SkillIndexTypeTemplate,
		Description: description,
		URL:         uriTemplate,
	})
	return s.Resource(uriTemplate).
		Name("skill-template").
		Description(description).
		MimeType(SkillMarkdownMIME)
}

func (b *SkillBuilder) loadSkillRoot(dir string) (root string, fm SkillFrontmatter, err error) {
	root, err = filepath.Abs(dir)
	if err != nil {
		return "", fm, fmt.Errorf("skill %q: resolve dir: %w", b.skillPath, err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fm, fmt.Errorf("skill %q: %w", b.skillPath, err)
	}
	if !info.IsDir() {
		return "", fm, fmt.Errorf("skill %q: %q is not a directory", b.skillPath, root)
	}
	entryPath, err := ContainedPath(root, SkillEntryPoint)
	if err != nil {
		return "", fm, fmt.Errorf("skill %q: %w", b.skillPath, err)
	}
	raw, err := os.ReadFile(entryPath) //nolint:gosec // ContainedPath keeps the path under root.
	if err != nil {
		return "", fm, fmt.Errorf("skill %q: read %s: %w", b.skillPath, SkillEntryPoint, err)
	}
	fm, err = parseSkillFrontmatter(string(raw))
	if err != nil {
		return "", fm, fmt.Errorf("skill %q: %w", b.skillPath, err)
	}
	final := filepath.Base(b.skillPath)
	if fm.Name != final {
		return "", fm, fmt.Errorf("skill %q: frontmatter name %q must equal final path segment %q", b.skillPath, fm.Name, final)
	}
	return root, fm, nil
}

func listSkillFiles(root, skillPath string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.Contains(rel, "..") {
			return fmt.Errorf("skill %q: refusing path %q", skillPath, rel)
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("skill %q: walk: %w", skillPath, err)
	}
	return files, nil
}

func (b *SkillBuilder) registerSkillFile(root, rel string, fm SkillFrontmatter) {
	uri := skillFileURI(b.skillPath, rel)
	mime := skillMIME(rel)
	builder := b.server.Resource(uri).MimeType(mime)
	if rel == SkillEntryPoint {
		builder = builder.Name(fm.Name).Description(fm.Description)
	} else {
		builder = builder.Name(rel)
	}
	builder.Handler(func(_ context.Context, reqURI string, _ map[string]string) (*ResourceContent, error) {
		path, err := ContainedPath(root, rel)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path) //nolint:gosec // ContainedPath keeps the path under root.
		if err != nil {
			return nil, err
		}
		out := &ResourceContent{URI: reqURI, MimeType: mime}
		if isTextMIME(mime) {
			out.Text = string(data)
		} else {
			out.Blob = base64.StdEncoding.EncodeToString(data)
		}
		return out, nil
	})
}

// SkillsFromDir registers every immediate subdirectory of root that contains
// SKILL.md, using the directory name as the skill path.
//
// Experimental: SEP-2640 is still in definition; see the package comment.
func (s *Server) SkillsFromDir(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("skills from dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, SkillEntryPoint)); err != nil {
			continue
		}
		if err := s.Skill(e.Name()).FromDir(dir); err != nil {
			return err
		}
	}
	return nil
}

// HasSkills reports whether any skill has been registered (advertises
// io.modelcontextprotocol/skills).
//
// Experimental: SEP-2640 is still in definition; see the package comment.
func (s *Server) HasSkills() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.skills != nil && len(s.skills.entries) > 0
}

func (s *Server) addSkillIndexEntry(entry SkillIndexEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.skills == nil {
		s.skills = &skillRegistry{}
	}
	for i, e := range s.skills.entries {
		if e.URL == entry.URL {
			s.skills.entries[i] = entry
			s.ensureSkillIndexLocked()
			return
		}
	}
	s.skills.entries = append(s.skills.entries, entry)
	s.ensureSkillIndexLocked()
}

// ensureSkillIndexLocked registers skill://index.json once. Caller holds s.mu.
func (s *Server) ensureSkillIndexLocked() {
	if s.skills == nil || s.skills.indexed {
		return
	}
	s.skills.indexed = true
	r := &Resource{
		uriTemplate: SkillIndexURI,
		name:        "skills-index",
		description: "Agent Skills discovery index (SEP-2640)",
		mimeType:    SkillIndexMIME,
		handler: func(_ context.Context, uri string, _ map[string]string) (*ResourceContent, error) {
			idx := s.skillIndexSnapshot()
			data, err := json.Marshal(idx)
			if err != nil {
				return nil, err
			}
			return &ResourceContent{URI: uri, MimeType: SkillIndexMIME, Text: string(data)}, nil
		},
	}
	if err := r.compileTemplate(); err != nil {
		s.regErrs = append(s.regErrs, fmt.Errorf("skill index: %w", err))
		return
	}
	if _, exists := s.resources[r.uriTemplate]; exists {
		return
	}
	s.resources[r.uriTemplate] = r
}

func (s *Server) skillIndexSnapshot() SkillIndex {
	s.mu.RLock()
	defer s.mu.RUnlock()
	idx := SkillIndex{Schema: SkillIndexSchema}
	if s.skills == nil {
		return idx
	}
	idx.Skills = append([]SkillIndexEntry(nil), s.skills.entries...)
	return idx
}

func skillFileURI(skillPath, rel string) string {
	return SkillScheme + "://" + skillPath + "/" + filepath.ToSlash(rel)
}

// validateSkillPath checks skill-path segments. The final segment must be a
// valid Agent Skills name (lowercase letters, digits, hyphens). Prefix segments
// are organizational and only need to be non-empty URI path segments.
func validateSkillPath(skillPath string) error {
	parts := strings.Split(skillPath, "/")
	if len(parts) == 0 {
		return fmt.Errorf("skill: empty path")
	}
	for i, p := range parts {
		if p == "" || p == "." || p == ".." {
			return fmt.Errorf("skill: invalid path segment %q", p)
		}
		if i == len(parts)-1 {
			if err := validateSkillName(p); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("skill: empty name")
	}
	for _, r := range name {
		if unicode.IsLower(r) || unicode.IsDigit(r) || r == '-' {
			continue
		}
		return fmt.Errorf("skill: name %q must use lowercase letters, digits, and hyphens only", name)
	}
	return nil
}

// parseSkillFrontmatter reads the YAML frontmatter between leading --- fences.
// Only name and description are required; other keys are ignored.
func parseSkillFrontmatter(content string) (SkillFrontmatter, error) {
	var zero SkillFrontmatter
	body := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(body, "---") {
		return zero, fmt.Errorf("%s missing YAML frontmatter", SkillEntryPoint)
	}
	rest := strings.TrimPrefix(body, "---")
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return zero, fmt.Errorf("%s frontmatter not closed", SkillEntryPoint)
	}
	fm := applyFrontmatterLines(strings.Split(rest[:end], "\n"))
	if fm.Name == "" {
		return zero, fmt.Errorf("%s frontmatter missing name", SkillEntryPoint)
	}
	if fm.Description == "" {
		return zero, fmt.Errorf("%s frontmatter missing description", SkillEntryPoint)
	}
	if err := validateSkillName(fm.Name); err != nil {
		return zero, err
	}
	return fm, nil
}

func applyFrontmatterLines(lines []string) SkillFrontmatter {
	var fm SkillFrontmatter
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case frontmatterKeyName:
			fm.Name = unquoteYAML(val)
		case frontmatterKeyDescription:
			fm.Description, i = readFrontmatterValue(val, lines, i)
		}
	}
	return fm
}

func readFrontmatterValue(val string, lines []string, i int) (string, int) {
	if val != "|" && val != ">" {
		return unquoteYAML(val), i
	}
	var b strings.Builder
	for i+1 < len(lines) {
		next := lines[i+1]
		if next != "" && !strings.HasPrefix(next, " ") && !strings.HasPrefix(next, "\t") {
			break
		}
		i++
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimSpace(next))
	}
	return b.String(), i
}

func unquoteYAML(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

func skillMIME(rel string) string {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".md", ".markdown":
		return SkillMarkdownMIME
	case ".json":
		return SkillIndexMIME
	case ".txt":
		return mimeTextPlain
	case ".py":
		return "text/x-python"
	case ".js":
		return "text/javascript"
	case ".ts":
		return "text/typescript"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".png":
		return mimeImagePNG
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return mimeImageSVG
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return SkillArchiveMIMEZip
	case ".gz":
		return SkillArchiveMIMEGzip
	default:
		return "application/octet-stream"
	}
}

func isTextMIME(mime string) bool {
	if strings.HasPrefix(mime, "text/") {
		return true
	}
	switch mime {
	case SkillIndexMIME, "application/yaml", mimeImageSVG:
		return true
	default:
		return false
	}
}
