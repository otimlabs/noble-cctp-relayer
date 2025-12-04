package types

// AttestationResponse is the v1 API response format
type AttestationResponse struct {
	Attestation string `json:"attestation"`
	Status      string `json:"status"`
}

// AttestationResponseV2 is the v2 API response format
type AttestationResponseV2 struct {
	Messages []MessageResponseV2 `json:"messages"`
}

// MessageResponseV2 represents a message in v2 response
type MessageResponseV2 struct {
	Message                   string `json:"message"`
	Attestation               string `json:"attestation"`
	Status                    string `json:"status"`
	EventNonce                string `json:"eventNonce"`
	SourceDomain              string `json:"sourceDomain"`
	DestinationDomain         string `json:"destinationDomain"`
	CctpVersion               string `json:"cctpVersion"`
	FinalityThresholdExecuted string `json:"finalityThresholdExecuted"`
	ExpirationBlock           string `json:"expirationBlock"`
}

// FastTransferAllowance is the v2 allowance response
type FastTransferAllowance struct {
	SourceDomain string `json:"sourceDomain"`
	Token        string `json:"token"`
	Allowance    string `json:"allowance"`
	MaxAllowance string `json:"maxAllowance"`
}

// ReattestResponse is the v2 re-attestation response
type ReattestResponse struct {
	Message     string `json:"message"`
	Attestation string `json:"attestation"`
	Status      string `json:"status"`
}
