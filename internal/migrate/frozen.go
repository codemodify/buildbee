package migrate

// frozen pins every released migration by checksum. A released file never
// changes, or databases that applied it could no longer be upgraded:
// schema changes ship as a new file (0002_..., 0003_...), whose checksum is
// added here in the same commit. TestMigrationsAreFrozen enforces both.
var frozen = map[string]string{
	"0001_baseline.sql": "f271834b3828e0e2dcc7df52bbd02494e481922691f6604034f91a9264c022bc",
}
