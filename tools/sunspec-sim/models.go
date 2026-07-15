package main

// SunSpec model register layouts. Each list is the DATA block of one model, in register order,
// exactly as the published SunSpec model definitions specify. The block length (the number a
// reader finds in the model's length register) is the sum of the register counts below, and it is
// asserted against the spec's fixed length in models_test.go so a mislaid field cannot silently
// shift every point after it.
//
// Points we do not simulate are kept as pad() of the correct width rather than dropped: a reader
// walking the chain by length must find every register the spec says is there.

// Model 1 — Common (nameplate). Fixed length 66.
func commonPoints() []point {
	return []point{
		str("Mn", 16), str("Md", 16), str("Opt", 8), str("Vr", 8), str("SN", 16),
		u16("DA"), pad(1),
	}
}

// Model 103 — Inverter, three-phase, integer + scale factor. Fixed length 50.
func inverterPoints() []point {
	return []point{
		u16("A"), u16("AphA"), u16("AphB"), u16("AphC"), sf("A_SF"),
		u16("PPVphAB"), u16("PPVphBC"), u16("PPVphCA"), u16("PhVphA"), u16("PhVphB"), u16("PhVphC"), sf("V_SF"),
		i16("W"), sf("W_SF"),
		u16("Hz"), sf("Hz_SF"),
		i16("VA"), sf("VA_SF"),
		i16("VAr"), sf("VAr_SF"),
		i16("PF"), sf("PF_SF"),
		u32("WH"), sf("WH_SF"),
		u16("DCA"), sf("DCA_SF"),
		u16("DCV"), sf("DCV_SF"),
		i16("DCW"), sf("DCW_SF"),
		i16("TmpCab"), i16("TmpSnk"), i16("TmpTrns"), i16("TmpOt"), sf("Tmp_SF"),
		u16("St"), u16("StVnd"),
		u32("Evt1"), u32("Evt2"), u32("EvtVnd1"), u32("EvtVnd2"), u32("EvtVnd3"), u32("EvtVnd4"),
	}
}

// Model 123 — Immediate Inverter Controls. Fixed length 24. WMaxLimPct + WMaxLim_Ena are the
// export-curtailment control this simulator honours (writing them throttles the inverter).
func controlPoints() []point {
	return []point{
		u16("Conn_WinTms"), u16("Conn_RvrtTms"), u16("Conn"),
		u16("WMaxLimPct"), u16("WMaxLimPct_WinTms"), u16("WMaxLimPct_RvrtTms"), u16("WMaxLimPct_RmpTms"), u16("WMaxLim_Ena"),
		i16("OutPFSet"), u16("OutPFSet_WinTms"), u16("OutPFSet_RvrtTms"), u16("OutPFSet_RmpTms"), u16("OutPFSet_Ena"),
		i16("VArWMaxPct"), i16("VArMaxPct"), i16("VArAvalPct"),
		u16("VArPct_WinTms"), u16("VArPct_RvrtTms"), u16("VArPct_RmpTms"), u16("VArPct_Mod"), u16("VArPct_Ena"),
		sf("WMaxLimPct_SF"), sf("OutPFSet_SF"), sf("VArPct_SF"),
	}
}

// Model 124 — Storage (battery control). Fixed length 24. ChaState is state-of-charge; StorCtl_Mod
// is the writable charge/discharge-enable bitfield this simulator honours.
func storagePoints() []point {
	return []point{
		u16("WChaMax"), u16("WChaGra"), u16("WDisChaGra"), u16("StorCtl_Mod"), u16("VAChaMax"),
		u16("MinRsvPct"), u16("ChaState"), u16("StorAval"), u16("InBatV"), u16("ChaSt"),
		i16("OutWRte"), i16("InWRte"), u16("InOutWRte_WinTms"), u16("InOutWRte_RvrtTms"), u16("InOutWRte_RmpTms"), u16("ChaGriSet"),
		sf("WChaMax_SF"), sf("WChaDisChaGra_SF"), sf("VAChaMax_SF"), sf("MinRsvPct_SF"), sf("ChaState_SF"), sf("StorAval_SF"), sf("InBatV_SF"), sf("InOutWRte_SF"),
	}
}

// Model 203 — Wye-connect three-phase meter, integer + scale factor. Fixed length 105. Only the
// points we simulate (totals, power, energy import/export) are named; the per-phase and VAh/VARh
// accumulator blocks are pad() so the chain length still matches the spec.
func meterPoints() []point {
	return []point{
		i16("A"), pad(3), sf("A_SF"), //                                   current block
		i16("PhV"), pad(3), i16("PPV"), pad(3), sf("V_SF"), //             voltage block
		i16("Hz"), sf("Hz_SF"),
		i16("W"), pad(3), sf("W_SF"),
		i16("VA"), pad(3), sf("VA_SF"),
		i16("VAR"), pad(3), sf("VAR_SF"),
		i16("PF"), pad(3), sf("PF_SF"),
		u32("TotWhExp"), pad(6), u32("TotWhImp"), pad(6), sf("TotWh_SF"), // real-energy accumulators
		pad(16), sf("TotVAh_SF"), //                                       apparent-energy block (unused)
		pad(32), sf("TotVArh_SF"), //                                      reactive-energy block (unused)
		u32("Evt"),
	}
}
