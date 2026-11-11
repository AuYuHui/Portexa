package storage

// ClockObservation ties a raw close timestamp to an independently observed UTC
// interval. A historical timestamp by itself is never evidence of UTC offset.
type ClockObservation struct {
	ID, AccountID, ServerID, EndpointID string
	ConfigVersion                       int64
	BeforeSnapshotID, AfterSnapshotID   string
	Ticket                              int32
	Command                             int32
	Symbol                              string
	OpenTimeWall                        uint32
	WindowFromMS, WindowToMS            int64
	State                               string
	CloseTimeWall                       *uint32
	OffsetMinutes                       *int
	CheckedAtMS                         *int64
}

func (ClockObservation) TableName() string { return "trade_clock_observations" }

type ClockCalibration struct {
	ServerID                        string `gorm:"primaryKey"`
	Status                          string
	OffsetMinutes                   *int
	EvidenceCount, EventWindowCount int
	ObservedAtMS, ExpiresAtMS       *int64
}

func (ClockCalibration) TableName() string { return "server_clock_calibrations" }

type ClockRun struct {
	ID                              string `gorm:"primaryKey"`
	AccountID, ServerID, EndpointID string
	ConfigVersion                   int64
	WindowStart, WindowEnd          uint32
	State                           string
	StartedAtMS                     int64
	FinishedAtMS                    *int64
	ErrorCode, CompletionReason     *string
}

func (ClockRun) TableName() string { return "clock_calibration_runs" }
