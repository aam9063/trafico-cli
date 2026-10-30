package datex2

import "time"

// Publication is the top-level result of parsing an incident payload
// (sit:SituationPublication). PublishedAt comes from <com:publicationTime>
// and is the only valid freshness source (docs/feeds.md §2); it is
// normalised to UTC (the feed sends +01:00/+02:00 offsets).
type Publication struct {
	PublishedAt time.Time
	Situations  []Situation
}

// Situation mirrors <sit:situation>: one situation groups 1..N records.
// OverallSeverity is the raw <sit:overallSeverity> value ("" when absent).
type Situation struct {
	ID              string
	OverallSeverity string
	Records         []Record
}

// Record mirrors one <sit:situationRecord>: the unit that carries location
// and cause (docs/feeds.md §4.3). Every raw DATEX II value is preserved so
// the mapping can be wrong without losing the source text (P-04).
type Record struct {
	// ID is <sit:situationRecord/@id>; SituationID is the parent
	// <sit:situation/@id>. Both ids exist in the feed (§4.3).
	ID          string
	SituationID string
	// Version is the @version attribute of the record element.
	Version string
	// RecordType is the local name of the record's xsi:type attribute
	// (e.g. "RoadOrCarriagewayOrLaneManagement"). The namespace prefix is
	// stripped because prefixes are arbitrary per payload.
	RecordType string
	// LocationType is the local name of the xsi:type of
	// <sit:locationReference>: "SingleRoadLinearLocation" or "PointLocation".
	LocationType string

	// CreationTime is <sit:situationRecordCreationTime> and VersionTime is
	// <sit:situationRecordVersionTime> (the updated_at of the record).
	// Both are normalised to UTC.
	CreationTime time.Time
	VersionTime  time.Time

	// SeverityRaw is the raw severity actually published: <sit:severity> of
	// the record, falling back to <sit:overallSeverity> of the situation
	// (docs/feeds.md §4.5). "" when neither is present; use Mapping.Severity
	// to normalise. It is never inferred from the cause.
	SeverityRaw string

	// CauseType is <sit:causeType>. DetailedCauseTypeElement/Value are the
	// element name and text of the child of <sit:detailedCauseType>
	// (e.g. "roadMaintenanceType" / "roadworks").
	CauseType                string
	DetailedCauseTypeElement string
	DetailedCauseTypeValue   string

	// ValidityStatus is <com:validityStatus> ("active" in everything
	// measured so far); status itself must be computed from the validity
	// window, not from this field.
	ValidityStatus string
	// StartTime is <com:overallStartTime> (present in 100% of records).
	// EndTime is <com:overallEndTime>; nil in ~35% of records. Both UTC.
	StartTime time.Time
	EndTime   *time.Time

	// RoadName is loc:roadInformation/loc:roadName (present in 100% of
	// records, docs/feeds.md §4.2).
	RoadName string
	// Direction is lse:tpegDirectionRoad: positive, negative or both — the
	// direction of travel (§4.4).
	Direction string
	// DirectionBearing is loc:tpegDirection: a compass bearing such as
	// "westBound". It is a separate concept from Direction and must never be
	// exposed as the road direction (§4.4).
	DirectionBearing string
	// LaneUsages lists the loc:laneUsage values. There is no lane total in
	// the feed, only the affected lanes (§4.3).
	LaneUsages []string

	// PK holds lse:kilometerPoint of the from/to points. For a point
	// location only From is set. Nil when the payload carries no kilometre
	// point at all.
	PK *PK
	// Start and End are the loc:pointCoordinates of the from and to points;
	// for a point location only Start is set. Nil when absent.
	Start *Coordinates
	End   *Coordinates

	// Province, Municipality and AutonomousCommunity come from the
	// lse: extension of the location points; present in 100% of records.
	Province            string
	Municipality        string
	AutonomousCommunity string
}

// PK is the kilometre-point interval of a location ("punto kilométrico").
type PK struct {
	From *float64
	To   *float64
}

// Coordinates is a WGS84 position from loc:pointCoordinates.
type Coordinates struct {
	Latitude  float64
	Longitude float64
}
