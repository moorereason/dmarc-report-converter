package main

import (
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v2"
)

type config struct {
	Input        Input  `yaml:"input"`
	Output       Output `yaml:"output"`
	LookupAddr   bool   `yaml:"lookup_addr"`
	LookupLimit  int    `yaml:"lookup_limit"`
	MergeReports bool   `yaml:"merge_reports"`
	MergeKey     string `yaml:"merge_key"`
	LogDebug     bool   `yaml:"log_debug"`
	LogDatetime  bool   `yaml:"log_datetime"`
}

// Input is the input section of config
type Input struct {
	Dir        string `yaml:"dir"`
	IMAP       IMAP   `yaml:"imap"`
	Delete     bool   `yaml:"delete"`
	ArchiveDir string `yaml:"archive_dir"`
}

// IMAP is the input.imap section of config
type IMAP struct {
	Server   string `yaml:"server"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Mailbox  string `yaml:"mailbox"`
	Debug    bool   `yaml:"debug"`
	Delete   bool   `yaml:"delete"`
	Security string `yaml:"security"`
}

// IsConfigured return true if IMAP is configured
func (i IMAP) IsConfigured() bool {
	return i.Server != ""
}

// Output is the output section of config
type Output struct {
	File             string `yaml:"file"`
	fileTemplate     *template.Template
	Format           string `yaml:"format"`
	Template         string
	template         *template.Template
	AssetsPath       string `yaml:"assets_path"`
	ExternalTemplate string `yaml:"external_template"`
	mergeKeyTemplate *template.Template
}

func (o *Output) isStdout() bool {
	if o.File == "" || o.File == "stdout" {
		return true
	}

	return false
}

func loadConfig(path string) (*config, error) {
	var c config

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(data, &c)
	if err != nil {
		return nil, err
	}

	if c.LookupLimit < 1 {
		c.LookupLimit = 50
	}

	if c.Input.Dir == "" {
		return nil, fmt.Errorf("input.dir is not configured")
	}

	if c.Input.Delete && c.Input.ArchiveDir != "" {
		return nil, fmt.Errorf("input.delete and input.archive_dir are mutually exclusive")
	}

	c.Input.Dir = filepath.Clean(c.Input.Dir)
	c.Input.ArchiveDir = filepath.Clean(c.Input.ArchiveDir)
	c.Output.ExternalTemplate = filepath.Clean(c.Output.ExternalTemplate)

	if c.Input.Dir == c.Input.ArchiveDir {
		return nil, fmt.Errorf("input.dir and input.archive_dir are the same location")
	}

	if c.Input.IMAP.Security == "" {
		c.Input.IMAP.Security = "tls"
	}

	if c.Input.IMAP.Security != "tls" && c.Input.IMAP.Security != "starttls" && c.Input.IMAP.Security != "plaintext" {
		return nil, fmt.Errorf("'input.imap.security' must be one of: tls, starttls, plaintext")
	}

	if c.MergeKey == "" {
		c.MergeKey = `{{ .ReportMetadata.OrgName }}!{{ .ReportMetadata.Email }}!{{ .PolicyPublished.Domain }}`
	}

	// Determine which template is used based upon Output.Format.
	t := txtTmpl
	switch c.Output.Format {
	case "txt":
	case "html":
		t = htmlTmpl
	case "html_static":
		t = htmlStaticTmpl
	case "external_template":
		if c.Output.ExternalTemplate == "" {
			return nil, fmt.Errorf("'output.external_template' must be configured to use external_template output")
		}
		data, err := os.ReadFile(c.Output.ExternalTemplate)
		if err != nil {
			return nil, err
		}
		t = string(data)
	case "json":
	default:
		return nil, fmt.Errorf("unable to found template for format '%v' in templates folder", c.Output.Format)
	}
	tmplFuncs := template.FuncMap{
		"now": func(fmt string) string {
			return time.Now().Format(fmt)
		},
	}

	c.Output.template = template.Must(template.New("report").Funcs(tmplFuncs).Parse(t))

	if !c.Output.isStdout() {
		// load and parse output filename template
		ft := template.Must(template.New("filename").Funcs(tmplFuncs).Parse(c.Output.File))
		c.Output.fileTemplate = ft
	}

	c.Output.mergeKeyTemplate = template.Must(template.New("merge_key").Funcs(tmplFuncs).Parse(c.MergeKey))

	err = os.MkdirAll(c.Input.ArchiveDir, 0700)
	if err != nil && !errors.Is(err, fs.ErrExist) {
		log.Fatalf("[ERROR] error created archive path: %s", err)
	}

	return &c, nil
}
