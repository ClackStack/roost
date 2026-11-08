package extract

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

func extractXlsx(src Source, size int64, lim Limits) (*Result, error) {
	if err := checkEncrypted(src); err != nil {
		return nil, err
	}
	// Keeping both limits equal holds everything in memory. Otherwise excelize
	// spills large sheets to plaintext temp files, which would outlive a killed
	// worker and put document contents outside the data directory.
	f, err := excelize.OpenReader(io.NewSectionReader(src, 0, size), excelize.Options{
		UnzipSizeLimit:    lim.MaxUnzip,
		UnzipXMLSizeLimit: lim.MaxUnzip,
	})
	if err != nil {
		if errors.Is(err, excelize.ErrWorkbookFileFormat) {
			return nil, fmt.Errorf("not a valid workbook: %w", err)
		}
		return nil, err
	}
	defer f.Close()

	c := newCollector(lim, "")
	if props, err := f.GetDocProps(); err == nil && props != nil {
		c.res.Title = strings.TrimSpace(clean(props.Title))
		setMeta(&c.res, "author", props.Creator)
		setMeta(&c.res, "subject", props.Subject)
		setMeta(&c.res, "created", props.Created)
		setMeta(&c.res, "modified", props.Modified)
		setMeta(&c.res, "last_modified_by", props.LastModifiedBy)
	}
	sheets := f.GetSheetList()
	setMeta(&c.res, "sheets", strconv.Itoa(len(sheets)))
	for _, sheet := range sheets {
		if c.full() {
			break
		}
		label := "Sheet: " + sheet
		// Hidden sheets are indexed, since sensitive data is often parked in
		// them, but they are labelled so readers know it is not on display.
		if visible, err := f.GetSheetVisible(sheet); err == nil && !visible {
			label += " (hidden)"
		}
		c.section(label)
		if err := readSheet(f, sheet, c); err != nil {
			setMeta(&c.res, "extraction_warning", fmt.Sprintf("sheet %q: %v", sheet, err))
		}
	}
	return c.result(), nil
}

func readSheet(f *excelize.File, sheet string, c *collector) error {
	rows, err := f.Rows(sheet)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() && !c.full() {
		cols, err := rows.Columns()
		if err != nil {
			return err
		}
		for len(cols) > 0 && strings.TrimSpace(cols[len(cols)-1]) == "" {
			cols = cols[:len(cols)-1]
		}
		if len(cols) == 0 {
			continue
		}
		for i := range cols {
			cols[i] = strings.Join(strings.Fields(cols[i]), " ")
		}
		c.line(strings.Join(cols, " | "))
	}
	return rows.Error()
}
