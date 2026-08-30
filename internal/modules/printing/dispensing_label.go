// Package printing renders printable documents hospital-api owns. Unlike pos-api's original
// dispensing label (a raw ESC/POS byte stream fed through its local print-agent job queue —
// see pos-service/pos-api's now-deleted internal/modules/printing/dispensing_label.go),
// hospital-api has no print-agent/printer-profile infrastructure of its own to port, so this
// renders a small PDF page instead: a real, immediately-usable capability (open/print from any
// browser, no local agent required) rather than porting pos-api's whole thermal-print-queue
// subsystem for one label type. Uses github.com/go-pdf/fpdf, the same library the platform's
// other PDF-producing services (pos/inventory/treasury) already depend on.
package printing

import (
	"bytes"
	"fmt"
	"time"

	"github.com/go-pdf/fpdf"
)

// DispensingLabelData holds everything printed on one drug line's dispensing label.
type DispensingLabelData struct {
	TenantName     string
	DrugName       string
	Dosage         string
	Form           string
	Instructions   string
	Quantity       float64
	PatientName    string
	LotNumber      string
	ExpiryDate     *time.Time
	PrescriberName string
	DispensedBy    string
	DispensedAt    time.Time
	PrescriptionNo string
}

// labelWidthMM/labelHeightMM match an 80mm thermal label roll's usable width, with enough
// height for every field below — printed via the browser's own print dialog onto a label
// printer set to that page size, or onto plain paper if no label printer is configured.
const (
	labelWidthMM  = 80.0
	labelHeightMM = 100.0
	margin        = 4.0
)

// BuildDispensingLabelPDF renders one drug line's dispensing label as a PDF byte buffer.
func BuildDispensingLabelPDF(d DispensingLabelData) ([]byte, error) {
	pdf := fpdf.NewCustom(&fpdf.InitType{
		OrientationStr: "P",
		UnitStr:        "mm",
		SizeStr:        "",
		Size:           fpdf.SizeType{Wd: labelWidthMM, Ht: labelHeightMM},
	})
	pdf.SetMargins(margin, margin, margin)
	pdf.AddPage()
	contentWidth := labelWidthMM - 2*margin

	pdf.SetFont("Helvetica", "B", 11)
	pdf.CellFormat(contentWidth, 5, sanitize(d.TenantName), "", 1, "C", false, 0, "")
	pdf.SetFont("Helvetica", "", 8)
	pdf.CellFormat(contentWidth, 4, "DISPENSING LABEL", "", 1, "C", false, 0, "")
	pdf.Ln(1)
	drawLine(pdf, contentWidth)

	pdf.SetFont("Helvetica", "B", 13)
	pdf.MultiCell(contentWidth, 5.5, sanitize(d.DrugName), "", "L", false)
	if d.Dosage != "" || d.Form != "" {
		pdf.SetFont("Helvetica", "B", 9)
		dosageLine := d.Dosage
		if d.Form != "" {
			if dosageLine != "" {
				dosageLine += " — "
			}
			dosageLine += d.Form
		}
		pdf.CellFormat(contentWidth, 4.5, sanitize(dosageLine), "", 1, "L", false, 0, "")
	}
	if d.Instructions != "" {
		pdf.SetFont("Helvetica", "", 9)
		pdf.MultiCell(contentWidth, 4.5, sanitize(d.Instructions), "", "L", false)
	}
	if d.Quantity > 0 {
		pdf.SetFont("Helvetica", "", 9)
		pdf.CellFormat(contentWidth, 4.5, fmt.Sprintf("Qty: %s", formatQty(d.Quantity)), "", 1, "L", false, 0, "")
	}
	pdf.Ln(1)
	drawLine(pdf, contentWidth)

	pdf.SetFont("Helvetica", "", 8)
	field := func(label, value string) {
		if value == "" {
			return
		}
		pdf.CellFormat(contentWidth, 4, fmt.Sprintf("%s: %s", label, sanitize(value)), "", 1, "L", false, 0, "")
	}
	field("Patient", d.PatientName)
	field("Batch", d.LotNumber)
	if d.ExpiryDate != nil {
		field("Expiry", d.ExpiryDate.Format("02 Jan 2006"))
	}
	field("Prescriber", d.PrescriberName)
	field("Dispensed by", d.DispensedBy)
	field("Date", d.DispensedAt.Format("02 Jan 2006 15:04"))
	if d.PrescriptionNo != "" {
		field("Rx No.", d.PrescriptionNo)
	}

	pdf.Ln(2)
	drawLine(pdf, contentWidth)
	pdf.SetFont("Helvetica", "B", 8)
	pdf.CellFormat(contentWidth, 4.5, "KEEP OUT OF REACH OF CHILDREN", "", 1, "C", false, 0, "")

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("printing: render dispensing label: %w", err)
	}
	return buf.Bytes(), nil
}

func drawLine(pdf *fpdf.Fpdf, width float64) {
	x, y := pdf.GetXY()
	pdf.Line(x, y, x+width, y)
	pdf.Ln(2)
}

func formatQty(q float64) string {
	if q == float64(int64(q)) {
		return fmt.Sprintf("%d", int64(q))
	}
	return fmt.Sprintf("%.2f", q)
}

// sanitize strips characters outside fpdf's default Helvetica (WinAnsi) encoding so a stray
// unicode character (e.g. a smart quote pasted into free-text instructions) never breaks
// rendering — fpdf silently mis-renders/omits glyphs outside the current font's encoding.
func sanitize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r < 32 && r != '\n' {
			continue
		}
		if r > 255 {
			out = append(out, '?')
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
