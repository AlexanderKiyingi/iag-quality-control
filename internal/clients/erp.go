package clients

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// ERP is iag-erp (HR): who is trained for which quality-control lab method.
// HR owns training; quality asks at the moment a result is recorded.
type ERP struct {
	baseClient
}

func NewERP(baseURL, tokenURL, clientID, clientSecret string) *ERP {
	return &ERP{baseClient: newBase(baseURL, tokenURL, clientID, clientSecret, "iag.erp")}
}

func (c *ERP) Enabled() bool { return c != nil && c.enabled() }

// Qualification is HR's record that a person completed a course qualifying
// them for a lab method.
type Qualification struct {
	EmployeeNo   string     `json:"employee_no"`
	EmployeeName string     `json:"employee_name"`
	Email        string     `json:"email"`
	UserID       string     `json:"user_id"`
	Method       string     `json:"method"`
	CourseCode   string     `json:"course_code"`
	CompletedOn  *time.Time `json:"completed_on"`
	ExpiresOn    *time.Time `json:"expires_on"`
	Expired      bool       `json:"expired"`
}

// Qualifications asks HR who is qualified, narrowed by email and/or method.
func (c *ERP) Qualifications(ctx context.Context, email, method string) ([]Qualification, error) {
	q := url.Values{}
	if email != "" {
		q.Set("email", email)
	}
	if method != "" {
		q.Set("method", method)
	}
	var out struct {
		Items []Qualification `json:"items"`
	}
	if _, _, err := c.doJSON(ctx, http.MethodGet, "/api/v1/training/qualifications?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}
