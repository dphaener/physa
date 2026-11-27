package initreport

// Why a measure can't be given: prose with no number, file path or name.
const (
	noGather         = "no gather is stored"
	noAreas          = "no areas are stored"
	noWalk           = "no walk is stored"
	noPlace          = "no placements are stored"
	noPlaceNoWalk    = "no placements are stored, since no walk is stored"
	noRows           = "no capability is recovered"
	noAreasForRules  = "no areas are stored, so a `from` path can't be told a test, a doc or a source file"
	noConsolidation  = "no consolidation is stored"
	staleCons        = "the stored consolidation no longer matches the recovered capabilities, since a recovery ran after it"
	noSpend          = "no spend is recorded"
	noSourceFiles    = "the stored areas hold no source file"
	noPRs            = "no PR is gathered"
	noFreeChanges    = "no gathered change stands outside a gathered PR"
	noOwnedPR        = "no gathered PR touches a source file a capability owns"
	noWalkCapability = "the stored walk holds no capability"
	noRules          = "the recovered capabilities hold no rule"
	noTestOnly       = "no rule rests on tests only"

	badGather        = "the stored gather can't be read"
	badAreas         = "the stored areas can't be read"
	badWalk          = "the stored walk can't be read"
	badPlace         = "the stored placements can't be read"
	badRows          = "the recovered capabilities can't be read"
	badConsolidation = "the stored consolidation can't be read"
	badSpend         = "the recorded spend can't be read"
)
