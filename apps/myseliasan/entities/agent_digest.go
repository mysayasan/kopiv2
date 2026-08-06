package entities

// AgentDigest is one generated fleet digest: the structured findings the
// deterministic narrator produced over a time window, plus the OPTIONAL language
// narrative the LLM wrote over those findings.
//
// FindingsJson is the record of substance — a JSON array of typed findings
// (code + params + grounding ids) that the frontend localizes into the viewer's
// language. Narrative is presentation only: prose in one fixed language, present
// only when an LLM was enabled AND healthy at generation time. A digest with an
// empty Narrative is not degraded data — it is the same findings, undecorated.
type AgentDigest struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// Kind is "daily" (scheduler) or "manual" (an operator's Generate-now).
	Kind string `json:"kind" form:"kind" query:"kind"`
	// PeriodStart/PeriodEnd bound the window (unix seconds) the findings cover.
	PeriodStart int64 `json:"periodStart" form:"periodStart" query:"periodStart"`
	PeriodEnd   int64 `json:"periodEnd" form:"periodEnd" query:"periodEnd" idx:"period_end"`
	// FindingsJson is the JSON array of services.Finding.
	FindingsJson string `json:"findingsJson" form:"findingsJson" query:"findingsJson"`
	// Severity is the max finding severity ("info"|"warning"|"critical"), denormalized
	// so history lists can badge rows without decoding FindingsJson.
	Severity string `json:"severity" form:"severity" query:"severity"`
	// Narrative is optional LLM prose in NarrativeLang. NarrativeSource is "none"
	// (narrator only) or "llm"; Model records what wrote it.
	Narrative       string `json:"narrative" form:"narrative" query:"narrative"`
	NarrativeLang   string `json:"narrativeLang" form:"narrativeLang" query:"narrativeLang"`
	NarrativeSource string `json:"narrativeSource" form:"narrativeSource" query:"narrativeSource"`
	Model           string `json:"model" form:"model" query:"model"`
	// DurationMs is how long generation took (including any LLM call).
	DurationMs int64 `json:"durationMs" form:"durationMs" query:"durationMs"`
	// GeneratedBy is the operator's user id for manual digests, 0 for the scheduler.
	GeneratedBy int64 `json:"generatedBy" form:"generatedBy" query:"generatedBy"`
	CreatedBy   int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt   int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy   int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt   int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
