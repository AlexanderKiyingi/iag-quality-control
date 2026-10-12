package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/clients"
)

/*
Qualification to run a method (Phase 5). HR owns training; quality decides
what counts:

  - a completed, unexpired course that qualifies for the method (iag-erp), and
  - when the method names a controlled SOP, having read the version in force
    (025's read-and-understood).

QUALIFICATION_ENFORCEMENT decides what an unqualified result does: "block"
refuses it, "warn" (the default) records it with the reasons attached, "off"
does not ask. When HR cannot be reached, training is reported unchecked
rather than failed — a network blip does not stop the lab.
*/

// QualificationCheck is the answer for one person and one method.
type QualificationCheck struct {
	Checked   bool     `json:"checked"`
	Qualified bool     `json:"qualified"`
	Reasons   []string `json:"reasons,omitempty"`
	Note      string   `json:"note,omitempty"`
}

func (h *QC) enforcement() string {
	switch h.Cfg.QualificationEnforcement {
	case "off", "block":
		return h.Cfg.QualificationEnforcement
	default:
		return "warn"
	}
}

func (h *QC) checkQualification(ctx context.Context, email, methodID string) QualificationCheck {
	methodID = strings.TrimSpace(methodID)
	email = strings.ToLower(strings.TrimSpace(email))
	if h.enforcement() == "off" || methodID == "" || email == "" {
		return QualificationCheck{}
	}
	methods, err := h.Store.LabMethodSOPs(ctx, methodID)
	if err != nil || len(methods) == 0 {
		// Not a registered method: there is nothing to be qualified for.
		return QualificationCheck{}
	}
	m := methods[0]
	out := QualificationCheck{Checked: true, Qualified: true}
	if h.ERP.Enabled() {
		quals, err := h.ERP.Qualifications(ctx, email, methodID)
		switch {
		case err != nil:
			log.Printf("quality-control: qualification check for %s on %s: %v", email, methodID, err)
			out.Note = "HR could not be reached; training was not checked"
		default:
			current, lapsed := false, ""
			for _, q := range quals {
				if !q.Expired {
					current = true
				} else if q.ExpiresOn != nil {
					lapsed = q.ExpiresOn.Format("2006-01-02")
				}
			}
			if !current {
				out.Qualified = false
				if lapsed != "" {
					out.Reasons = append(out.Reasons, fmt.Sprintf("training for %s expired on %s", methodID, lapsed))
				} else {
					out.Reasons = append(out.Reasons, fmt.Sprintf("no completed training qualifies them for %s", methodID))
				}
			}
		}
	} else {
		out.Note = "HR is not connected (UPSTREAM_ERP); training was not checked"
	}
	if m.SopRef != "" && m.EffectiveVersion > 0 {
		read, err := h.Store.HasReadSOP(ctx, m.SopRef, m.EffectiveVersion, email)
		if err == nil && !read {
			out.Qualified = false
			out.Reasons = append(out.Reasons, fmt.Sprintf("has not read %s v%d", strings.ToUpper(m.SopRef), m.EffectiveVersion))
		}
	}
	return out
}

// Matrix cell statuses.
const (
	cellQualified  = "qualified"
	cellExpiring   = "expiring"
	cellExpired    = "expired"
	cellSOPUnread  = "sop_unread"
	cellNotTrained = "not_trained"
)

type matrixCell struct {
	Status    string `json:"status"`
	ExpiresOn string `json:"expires_on,omitempty"`
	Course    string `json:"course,omitempty"`
}

type matrixPerson struct {
	Email      string                `json:"email"`
	Name       string                `json:"name"`
	EmployeeNo string                `json:"employee_no,omitempty"`
	Cells      map[string]matrixCell `json:"cells"`
}

/*
QualificationMatrix serves people × methods: for each, qualified, expiring
within 30 days, expired, trained but the SOP in force unread, or not trained.
People are those HR has trained for any lab method, and anyone who has read a
method's SOP.
*/
func (h *QC) QualificationMatrix(c *gin.Context) {
	ctx := c.Request.Context()
	methods, err := h.Store.LabMethodSOPs(ctx, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list methods"})
		return
	}
	readers, err := h.Store.SOPReaders(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read acknowledgements"})
		return
	}
	var quals []clients.Qualification
	hrNote := ""
	if h.ERP.Enabled() {
		if quals, err = h.ERP.Qualifications(ctx, "", ""); err != nil {
			hrNote = "HR could not be reached; training is not shown"
			quals = nil
		}
	} else {
		hrNote = "HR is not connected (UPSTREAM_ERP); training is not shown"
	}
	people := map[string]*matrixPerson{}
	person := func(email, name, emp string) *matrixPerson {
		email = strings.ToLower(strings.TrimSpace(email))
		p := people[email]
		if p == nil {
			p = &matrixPerson{Email: email, Name: name, EmployeeNo: emp, Cells: map[string]matrixCell{}}
			people[email] = p
		}
		if p.Name == "" {
			p.Name = name
		}
		return p
	}
	soon := time.Now().UTC().AddDate(0, 0, 30)
	// Best training standing per person and method.
	rank := map[string]int{cellQualified: 4, cellExpiring: 3, cellExpired: 1}
	for _, q := range quals {
		if q.Email == "" {
			continue
		}
		p := person(q.Email, q.EmployeeName, q.EmployeeNo)
		cell := matrixCell{Status: cellQualified, Course: q.CourseCode}
		if q.ExpiresOn != nil {
			cell.ExpiresOn = q.ExpiresOn.Format("2006-01-02")
			if q.Expired {
				cell.Status = cellExpired
			} else if q.ExpiresOn.Before(soon) {
				cell.Status = cellExpiring
			}
		}
		if cur, ok := p.Cells[q.Method]; !ok || rank[cell.Status] > rank[cur.Status] {
			p.Cells[q.Method] = cell
		}
	}
	for _, m := range methods {
		if m.SopRef == "" || m.EffectiveVersion == 0 {
			continue
		}
		for email := range readers[strings.ToUpper(m.SopRef)][m.EffectiveVersion] {
			person(email, "", "")
		}
	}
	out := []matrixPerson{}
	for _, p := range people {
		for _, m := range methods {
			cell, trained := p.Cells[m.MethodID]
			if !trained && quals != nil {
				cell = matrixCell{Status: cellNotTrained}
			}
			if m.SopRef != "" && m.EffectiveVersion > 0 && !readers[strings.ToUpper(m.SopRef)][m.EffectiveVersion][p.Email] &&
				(cell.Status == cellQualified || cell.Status == cellExpiring || (quals == nil && cell.Status == "")) {
				cell.Status = cellSOPUnread
			}
			if cell.Status == "" {
				cell.Status = "unknown"
			}
			p.Cells[m.MethodID] = cell
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	c.JSON(http.StatusOK, gin.H{"methods": methods, "people": out, "enforcement": h.enforcement(), "hr_note": hrNote})
}

// QualificationCheckFor answers for the signed-in person (or ?email=) and a
// method, so a screen can warn before anything is recorded.
func (h *QC) QualificationCheckFor(c *gin.Context) {
	email := c.Query("email")
	if email == "" {
		email = actorOf(c)
	}
	c.JSON(http.StatusOK, gin.H{"email": email, "method": c.Query("method"), "enforcement": h.enforcement(),
		"check": h.checkQualification(c.Request.Context(), email, c.Query("method"))})
}
