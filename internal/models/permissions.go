package models

type PermissionDescriptor struct {
	Name        string
	Description string
}

func PermissionDescriptors() []PermissionDescriptor {
	return []PermissionDescriptor{
		{Name: "qc.view_samples", Description: "View sample log and details"},
		{Name: "qc.add_sample", Description: "Register lab samples"},
		{Name: "qc.change_sample", Description: "Update sample status"},
		{Name: "qc.record_tests", Description: "Record physical, chemical, and cupping tests"},
		{Name: "qc.view_lab_summary", Description: "View batch lab summaries"},
		{Name: "qc.issue_coa", Description: "Issue certificates of analysis"},
		{Name: "qc.view_coa", Description: "List and view CoA records"},
		{Name: "qc.request_certification", Description: "Start lot certification workflow"},
		{Name: "qc.approve_certification", Description: "Approve certification workflow stages"},
		{Name: "qc.view_instruments", Description: "View lab instruments and calibration"},
		{Name: "qc.change_instruments", Description: "Update instruments and calibration"},
		{Name: "qc.view_compliance", Description: "View compliance logs and CAPAs"},
		{Name: "qc.change_compliance", Description: "Record compliance logs and CAPAs"},
		{Name: "qc.view_reports", Description: "View lab day reports and dashboards"},
		{Name: "qc.view_scm_context", Description: "Proxy read batches, export lots, and farmers from SCM"},
		{Name: "qc.view_technicians", Description: "View lab technician registry"},
		{Name: "qc.change_technicians", Description: "Update lab technician registry"},
		{Name: "qc.search", Description: "Global lab entity search"},
		{Name: "qc.export_pdf", Description: "Export CoA, cupping forms, and day reports as PDF"},
		{Name: "qc.view_calendar", Description: "View certification and calibration calendar"},
		{Name: "qc.print_labels", Description: "Generate sample barcode labels"},
		{Name: "qc.view_analytics", Description: "View SPC and statistical analytics"},
		{Name: "qc.view_mes_context", Description: "Read MES production context for batches"},
		{Name: "qc.sync_instruments", Description: "Sync instrument telemetry from MES"},
		{Name: "qc.view_queues", Description: "View instrument and HPLC work queues"},
		{Name: "qc.view_custody", Description: "View sample chain-of-custody logs"},
		{Name: "qc.change_custody", Description: "Record sample chain-of-custody events"},
		{Name: "qc.view_external_audits", Description: "View external certification audit calendar"},
		{Name: "qc.change_external_audits", Description: "Manage external audit schedule"},
		{Name: "qc.export_audit_pack", Description: "Export bundled compliance audit pack"},
		{Name: "qc.admin.read", Description: "Staff audit and monitoring APIs"},
	}
}
