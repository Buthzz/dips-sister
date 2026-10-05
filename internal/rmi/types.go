// Package rmi menyediakan implementasi Remote Method Invocation (RMI)
// untuk pemanggilan method objek terdistribusi dan sinkronisasi kluster.
package rmi

// Peer menyimpan informasi alamat dan prioritas satu node dalam kluster.
type Peer struct {
	NodeID    string `json:"node_id"`
	GRPCAddr  string `json:"grpc_addr"`
	RMIAddr   string `json:"rmi_addr"`
	HTTPAddr  string `json:"http_addr"`
	Priority  int    `json:"priority"`
	IsLeader  bool   `json:"is_leader"`
	LastSeen  int64  `json:"last_seen"`
}

// ElectionArgs adalah parameter permintaan pemilihan koordinator (Bully Algorithm).
type ElectionArgs struct {
	CandidateID   string `json:"candidate_id"`
	CandidateAddr string `json:"candidate_addr"`
	CandidateRMI  string `json:"candidate_rmi"`
	Priority      int    `json:"priority"`
}

// ElectionReply adalah balasan dari kandidat yang menerima pesan pemilihan.
type ElectionReply struct {
	OK          bool   `json:"ok"` // True jika penerima masih hidup dan berprioritas lebih tinggi
	ResponderID string `json:"responder_id"`
	Priority    int    `json:"priority"`
}

// VictoryArgs adalah parameter pengumuman koordinator baru ke seluruh node.
type VictoryArgs struct {
	CoordinatorID   string `json:"coordinator_id"`
	CoordinatorAddr string `json:"coordinator_addr"` // Alamat gRPC
	CoordinatorHTTP string `json:"coordinator_http"` // Alamat HTTP REST
	CoordinatorRMI  string `json:"coordinator_rmi"`  // Alamat RMI
	Priority        int    `json:"priority"`
}

// VictoryReply adalah konfirmasi penerimaan pengumuman koordinator.
type VictoryReply struct {
	Acknowledged bool   `json:"acknowledged"`
	NodeID       string `json:"node_id"`
}

// HeartbeatArgs memuat parameter detak jantung dan sinkronisasi topologi via RMI.
type HeartbeatArgs struct {
	NodeID   string `json:"node_id"`
	RMIAddr  string `json:"rmi_addr"`
	GRPCAddr string `json:"grpc_addr"`
	Priority int    `json:"priority"`
}

// HeartbeatReply memuat balasan detak jantung beserta topologi peer terkini.
type HeartbeatReply struct {
	OK            bool   `json:"ok"`
	CoordinatorID string `json:"coordinator_id"`
	Peers         []Peer `json:"peers"`
}

// ProcessImageArgs adalah parameter pemanggilan remote method untuk komputasi citra via RMI.
type ProcessImageArgs struct {
	TaskID       string `json:"task_id"`
	JobID        string `json:"job_id"`
	ImageIndex   int    `json:"image_index"`
	ImageData    []byte `json:"image_data"`
	Filename     string `json:"filename"`
	ResizeWidth  int    `json:"resize_width"`
	ResizeHeight int    `json:"resize_height"`
	Grayscale    bool   `json:"grayscale"`
}

// ProcessImageReply adalah hasil eksekusi transformasi citra via RMI.
type ProcessImageReply struct {
	TaskID     string `json:"task_id"`
	Success    bool   `json:"success"`
	ResultData []byte `json:"result_data"`
	Filename   string `json:"filename"`
	Error      string `json:"error"`
	DurationMs int64  `json:"duration_ms"`
}

// PingArgs adalah parameter uji konektivitas remote object.
type PingArgs struct {
	SenderID string `json:"sender_id"`
}

// PingReply adalah informasi kesehatan remote object.
type PingReply struct {
	NodeID      string `json:"node_id"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	ActiveTasks int    `json:"active_tasks"`
	Timestamp   int64  `json:"timestamp"`
}

// EmptyArgs parameter kosong untuk pemanggilan tanpa argumen.
type EmptyArgs struct{}

// ClusterViewReply memuat tampilan topologi kluster.
type ClusterViewReply struct {
	CoordinatorID string `json:"coordinator_id"`
	Peers         []Peer `json:"peers"`
}
