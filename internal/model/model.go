package model

// Instance represents an Alibaba Cloud ECS instance
type Instance struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Status    string            `json:"status"`
	PrivateIP string            `json:"private_ip"`
	PublicIP  string            `json:"public_ip"`
	EIP       string            `json:"eip"`
	Region    string            `json:"region"`
	Zone      string            `json:"zone"`
	Tags      map[string]string `json:"tags,omitempty"`
	Profile   string            `json:"profile,omitempty"`
}

// Config holds Aliyun credentials and settings
type Config struct {
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
	Region          string `json:"region"`
	ProfileName     string `json:"name,omitempty"`
}

// CommandResult holds the result of a remote command execution
type CommandResult struct {
	Output   string
	ExitCode int
}

// InstanceDetail holds extended instance information
type InstanceDetail struct {
	InstanceType     string
	CPU              int
	Memory           int // in MB
	OSName           string
	CreationTime     string
	ExpiredTime      string
	ChargeType       string
	VpcID            string
	SecurityGroupIDs []string
}
