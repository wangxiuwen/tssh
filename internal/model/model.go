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
