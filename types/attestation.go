package types

// AttestationResponse is the response received from Circle's iris api
// Example: https://iris-api-sandbox.circle.com/attestations/0x85bbf7e65a5992e6317a61f005e06d9972a033d71b514be183b179e1b47723fe
type AttestationResponse struct {
	Attestation string `json:"attestation"`
	Status      string `json:"status"`
}

// AttestationResponseV2 is the response received from Circle's iris api v2 messages endpoint
// Example: https://iris-api-sandbox.circle.com/v2/messages/0?transactionHash=0x85bbf7e65a5992e6317a61f005e06d9972a033d71b514be183b179e1b47723fe
type AttestationResponseV2 struct {
	Messages []MessageResponseV2 `json:"messages"`
}

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

// FastTransferAllowance is the response received from Circle's iris api v2 fast transfer allowance endpoint
// Example: https://iris-api-sandbox.circle.com/v2/fastBurn/0xusdc/allowance?sourceDomain=0
type FastTransferAllowance struct {
	SourceDomain string `json:"sourceDomain"`
	Token        string `json:"token"`
	Allowance    string `json:"allowance"`
	MaxAllowance string `json:"maxAllowance"`
}

// ReattestResponse is the response received from Circle's iris api v2 re-attestation endpoint
// Example: https://iris-api-sandbox.circle.com/v2/reattest/0/12345
type ReattestResponse struct {
	Message     string `json:"message"`
	Attestation string `json:"attestation"`
	Status      string `json:"status"`
}
