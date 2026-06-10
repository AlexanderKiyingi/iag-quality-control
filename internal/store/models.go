package store

import "time"

type Sample struct {
	BusinessID      string     `json:"business_id"`
	BatchBusinessID string     `json:"batch_business_id"`
	SampleType      string     `json:"sample_type"`
	Status          string     `json:"status"`
	Priority        string     `json:"priority"`
	AssignedTech    string     `json:"assigned_tech"`
	TestsRequired   []string   `json:"tests_required"`
	Notes           string     `json:"notes"`
	ReceivedAt      time.Time  `json:"received_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type PhysicalTest struct {
	BusinessID       string    `json:"business_id"`
	SampleBusinessID string    `json:"sample_business_id"`
	BatchBusinessID  string    `json:"batch_business_id"`
	TechID           string    `json:"tech_id"`
	MoisturePct      *float64  `json:"moisture_pct,omitempty"`
	Screen18         *float64  `json:"screen_18,omitempty"`
	Screen17         *float64  `json:"screen_17,omitempty"`
	Screen16         *float64  `json:"screen_16,omitempty"`
	Screen15         *float64  `json:"screen_15,omitempty"`
	ScreenPan        *float64  `json:"screen_pan,omitempty"`
	BulkDensityGL    *float64  `json:"bulk_density_g_l,omitempty"`
	DefectCat1       int       `json:"defect_cat1"`
	DefectCat2       int       `json:"defect_cat2"`
	Status           string    `json:"status"`
	TestedAt         time.Time `json:"tested_at"`
}

type ChemicalTest struct {
	BusinessID       string    `json:"business_id"`
	SampleBusinessID string    `json:"sample_business_id"`
	BatchBusinessID  string    `json:"batch_business_id"`
	TechID           string    `json:"tech_id"`
	MoistureKF       *float64  `json:"moisture_kf,omitempty"`
	WaterActivity    *float64  `json:"water_activity,omitempty"`
	PH               *float64  `json:"ph,omitempty"`
	ChlorogenicPct   *float64  `json:"chlorogenic_pct,omitempty"`
	CaffeinePct      *float64  `json:"caffeine_pct,omitempty"`
	Status           string    `json:"status"`
	TestedAt         time.Time `json:"tested_at"`
}

type CuppingSession struct {
	BusinessID       string    `json:"business_id"`
	SampleBusinessID string    `json:"sample_business_id"`
	BatchBusinessID  string    `json:"batch_business_id"`
	SessionDate      string    `json:"session_date"`
	Scorers          []string  `json:"scorers"`
	Fragrance        float64   `json:"fragrance"`
	Flavor           float64   `json:"flavor"`
	Aftertaste       float64   `json:"aftertaste"`
	Acidity          float64   `json:"acidity"`
	Body             float64   `json:"body"`
	Balance          float64   `json:"balance"`
	Uniformity       float64   `json:"uniformity"`
	CleanCup         float64   `json:"cleancup"`
	Sweetness        float64   `json:"sweetness"`
	Overall          float64   `json:"overall"`
	DefectCat1       int       `json:"defect_cat1"`
	DefectCat2       int       `json:"defect_cat2"`
	TotalScore       float64   `json:"total_score"`
	Grade            string    `json:"grade"`
	Notes            string    `json:"notes"`
	Status           string    `json:"status"`
}

type BatchLabSummary struct {
	BatchBusinessID string     `json:"batch_business_id"`
	Moisture        *float64   `json:"moisture,omitempty"`
	WaterActivity   *float64   `json:"water_activity,omitempty"`
	CupScore        *float64   `json:"cup_score,omitempty"`
	Grade           string     `json:"grade"`
	Defects         *int       `json:"defects,omitempty"`
	Tester          string     `json:"tester"`
	LabDate         *string    `json:"lab_date,omitempty"`
	LatestSampleID  string     `json:"latest_sample_id,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type CoA struct {
	CoaNumber       string    `json:"coa_number"`
	LotBusinessID   string    `json:"lot_business_id"`
	BatchBusinessID string    `json:"batch_business_id,omitempty"`
	DocumentRef     string    `json:"document_ref"`
	IssuedBy        string    `json:"issued_by"`
	IssuedAt        time.Time `json:"issued_at"`
	Status          string    `json:"status"`
}

type CreateSampleInput struct {
	BatchBusinessID string
	SampleType      string
	Priority        string
	AssignedTech    string
	TestsRequired   []string
	Notes           string
	SampleID        string
}

type CreatePhysicalTestInput struct {
	SampleBusinessID string
	TechID           string
	MoisturePct      *float64
	Screen18         *float64
	Screen17         *float64
	Screen16         *float64
	Screen15         *float64
	ScreenPan        *float64
	BulkDensityGL    *float64
	DefectCat1       int
	DefectCat2       int
	Status           string
}

type CreateChemicalTestInput struct {
	SampleBusinessID string
	TechID           string
	MoistureKF       *float64
	WaterActivity    *float64
	PH               *float64
	ChlorogenicPct   *float64
	CaffeinePct      *float64
	Status           string
}

type CreateCuppingInput struct {
	SampleBusinessID string
	Scorers          []string
	Fragrance        float64
	Flavor           float64
	Aftertaste       float64
	Acidity          float64
	Body             float64
	Balance          float64
	Uniformity       float64
	CleanCup         float64
	Sweetness        float64
	Overall          float64
	DefectCat1       int
	DefectCat2       int
	Notes            string
}

type UpsertLabSummaryInput struct {
	BatchBusinessID string
	Moisture        *float64
	WaterActivity   *float64
	CupScore        *float64
	Grade           string
	Defects         *int
	Tester          string
	LatestSampleID  string
}

type CreateCoAInput struct {
	LotBusinessID   string
	BatchBusinessID string
	CoaNumber       string
	DocumentRef     string
	IssuedBy        string
}

type RecordLabResultInput struct {
	BatchBusinessID string
	Moisture        float64
	CupScore        float64
	Grade           string
	Tester          string
	Defects         float64
	WaterActivity   float64
}
