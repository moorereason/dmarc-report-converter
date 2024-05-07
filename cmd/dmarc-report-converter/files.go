package main

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/tierpod/dmarc-report-converter/pkg/dmarc"
)

type filesConverter struct {
	cfg          *config
	files        []string
	filesSuccess []string
	reports      []dmarc.Report
}

func newFilesConverter(cfg *config) (*filesConverter, error) {
	if _, err := os.Stat(cfg.Input.Dir); os.IsNotExist(err) {
		err := os.MkdirAll(cfg.Input.Dir, 0775)
		if err != nil {
			return nil, err
		}
	}

	return &filesConverter{cfg: cfg}, nil
}

func (c *filesConverter) ConvertWrite() error {
	err := c.find()
	if err != nil {
		return err
	}

	c.convert()

	if c.cfg.MergeReports {
		err = c.merge()
		if err != nil {
			return err
		}
	}

	err = c.write()
	if err != nil {
		return err
	}

	if c.cfg.Input.Delete {
		c.delete()
	} else if c.cfg.Input.ArchiveDir != "" {
		c.archive()
	}

	return nil
}

func (c *filesConverter) find() error {
	emlFiles, err := filepath.Glob(filepath.Join(c.cfg.Input.Dir, "*.eml"))
	if err != nil {
		return err
	}
	if len(emlFiles) > 0 {
		log.Printf("[INFO] files: found %d eml file(s), extract attachments to %v", len(emlFiles), c.cfg.Input.Dir)
		for _, eml := range emlFiles {
			br, err := os.Open(eml)
			if err != nil {
				log.Printf("[ERROR] files: unable to extract attachments from %v", eml)
				continue
			}

			isSuccess, err := extractAttachment(br, c.cfg.Input.Dir)
			if err != nil {
				log.Printf("[ERROR] files: %v, skip", err)
				continue
			}

			if isSuccess {
				if c.cfg.Input.Delete {
					log.Printf("[DEBUG] files: delete %v", eml)
					err := os.Remove(eml)
					if err != nil {
						log.Printf("[ERROR] files: %v", err)
						continue
					}
				} else if c.cfg.Input.ArchiveDir != "" {
					log.Printf("[DEBUG] files: archive %v", eml)
					newpath := filepath.Join(c.cfg.Input.ArchiveDir, filepath.Base(eml))
					err := os.Rename(eml, newpath)
					if err != nil {
						log.Printf("[ERROR] files: %v", err)
						continue
					}
				}
			}
			br.Close()
		}
	}

	// Walk Input.Dir for a list of files to process, skipping eml files.
	var files []string
	err = filepath.Walk(c.cfg.Input.Dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			// Process the root dir but don't recurse
			if path != c.cfg.Input.Dir {
				return filepath.SkipDir
			}
		} else if filepath.Ext(path) != ".eml" {
			files = append(files, path)
		}

		return nil
	})
	if err != nil {
		return err
	}

	log.Printf("[INFO] files: found %v input files in %v", len(files), c.cfg.Input.Dir)
	c.files = files
	return nil
}

func (c *filesConverter) convert() {
	var reports []dmarc.Report
	var filesSuccess []string

	for _, f := range c.files {
		file, err := os.Open(f)
		if err != nil {
			log.Printf("[ERROR] files: %v, skip", err)
			continue
		}

		report, err := readParse(file, f, c.cfg.LookupAddr, c.cfg.LookupLimit)
		if err != nil {
			file.Close()
			log.Printf("[ERROR] files: %v in file %v, skip", err, f)
			continue
		}
		file.Close()

		filesSuccess = append(filesSuccess, f)
		reports = append(reports, report)
	}

	c.filesSuccess = filesSuccess
	c.reports = reports
}

func (c *filesConverter) merge() error {
	reports, err := groupMergeReports(c.reports, c.cfg.Output.mergeKeyTemplate)
	if err != nil {
		return err
	}

	c.reports = reports
	return nil
}

func (c *filesConverter) archive() {
	for _, f := range c.filesSuccess {
		log.Printf("[INFO] files: archive %v", f)
		newpath := filepath.Join(c.cfg.Input.ArchiveDir, filepath.Base(f))
		err := os.Rename(f, newpath)
		if err != nil {
			log.Printf("[ERROR] files: %v, skip", err)
			continue
		}
	}
}

func (c *filesConverter) delete() {
	for _, f := range c.filesSuccess {
		log.Printf("[INFO] files: delete %v", f)
		err := os.Remove(f)
		if err != nil {
			log.Printf("[ERROR] files: %v, skip", err)
			continue
		}
	}
}

func (c *filesConverter) write() error {
	for _, report := range c.reports {
		o := newOutput(c.cfg)
		err := o.do(report)
		if err != nil {
			return err
		}
	}
	return nil
}
