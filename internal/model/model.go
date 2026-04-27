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
	VpcID     string            `json:"vpc_id,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
	Profile   string            `json:"profile,omitempty"`
}

// Config holds Aliyun credentials and settings
type Config struct {
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
	// SecurityToken (sts_token) is set when credentials are STS / CloudSSO.
	// Aliyun SDK 必须同时拿到 AK + SK + token, 否则会被服务端拒掉。
	SecurityToken string `json:"sts_token,omitempty"`
	Region        string `json:"region"`
	ProfileName   string `json:"name,omitempty"`
}

// CommandResult holds the result of a remote command execution
type CommandResult struct {
	Output   string
	ExitCode int
}

// InvocationStatus is a single-shot snapshot of a Cloud Assistant invocation.
// Returned by FetchInvocation; lets callers inspect a running or finished command
// without the blocking polling of RunCommand.
type InvocationStatus struct {
	InvokeID     string `json:"invoke_id"`
	InstanceID   string `json:"instance_id"`
	Status       string `json:"status"` // Running / Success / Failed / Stopped / Finished / Pending / PartialFailed
	Output       string `json:"output"` // base64-decoded remote stdout
	ExitCode     int    `json:"exit_code"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorInfo    string `json:"error_info,omitempty"`
	StartTime    string `json:"start_time,omitempty"`
	FinishedTime string `json:"finished_time,omitempty"`
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

// RedisInstance represents an Alibaba Cloud Redis (R-KVStore) instance
type RedisInstance struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	InstanceClass    string `json:"instance_class"`
	InstanceType     string `json:"instance_type"`
	EngineVersion    string `json:"engine_version"`
	ArchitectureType string `json:"architecture_type"`
	Capacity         int64  `json:"capacity_mb"`
	ConnectionDomain string `json:"connection_domain"`
	Port             int64  `json:"port"`
	PrivateIP        string `json:"private_ip"`
	VpcID            string `json:"vpc_id"`
	NetworkType      string `json:"network_type"`
	RegionID         string `json:"region_id"`
	ZoneID           string `json:"zone_id"`
	ChargeType       string `json:"charge_type"`
	CreateTime       string `json:"create_time"`
	EndTime          string `json:"end_time"`
	Connections      int64  `json:"connections"`
	Bandwidth        int64  `json:"bandwidth_mbps"`
	QPS              int64  `json:"qps"`
}

// GrafanaConfig holds Grafana connection settings for ARMS integration
type GrafanaConfig struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
}

// RDSInstance represents an Alibaba Cloud RDS (MySQL/PostgreSQL/...) instance
type RDSInstance struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	Engine           string `json:"engine"`
	EngineVersion    string `json:"engine_version"`
	InstanceClass    string `json:"instance_class"`
	ConnectionString string `json:"connection_string"`
	VpcID            string `json:"vpc_id"`
	NetworkType      string `json:"network_type"`
	RegionID         string `json:"region_id"`
	ZoneID           string `json:"zone_id"`
	PayType          string `json:"pay_type"`
	CreateTime       string `json:"create_time"`
	ExpireTime       string `json:"expire_time"`
	LockMode         string `json:"lock_mode"`
	Category         string `json:"category"`
}
