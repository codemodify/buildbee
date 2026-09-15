package migrate

// frozen pins every released migration by checksum. A released file never
// changes, or databases that applied it could no longer be upgraded:
// schema changes ship as a new file (0002_..., 0003_...), whose checksum is
// added here in the same commit. TestMigrationsAreFrozen enforces both.
var frozen = map[string]string{
	"0001_baseline.sql":       "f271834b3828e0e2dcc7df52bbd02494e481922691f6604034f91a9264c022bc",
	"0002_message_edits.sql":  "91953d3fc97f8769c0c52a188ac9b066b8283c5d4680c15019ece7b41fcacc16",
	"0003_message_search.sql": "ea2aa4f2555a718e019468101a191f45d46bd0b0853854fe4c2e933be0099334",
	"0004_files.sql":          "69fe6682ceab7edc1e56ae6457baf7e096891a89168a85edf24e973ea7d6074b",
	"0005_project_images.sql": "aa99bb64a4fa91e797c8a2df7943e9fb3189c574037a69639cf58bd98950ccf2",
}
