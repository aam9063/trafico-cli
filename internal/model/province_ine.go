package model

import "strings"

// provinceINE maps the two-digit INE province code (the code the DGT radar
// feed publishes as provinceINEIdentifier, docs/feeds.md §6) to the
// province name, all 52 entries: the 50 provinces plus Ceuta (51) and
// Melilla (52).
//
// Where the official name is bilingual, the entry uses the form the
// incident feed publishes (docs/feeds.md §4.6: Alacant/Alicante,
// Castelló/Castellón, València/Valencia), so --province folds and matches
// the same way in every command through ProvinceEqual.
var provinceINE = map[string]string{
	"01": "Araba/Álava",
	"02": "Albacete",
	"03": "Alacant/Alicante",
	"04": "Almería",
	"05": "Ávila",
	"06": "Badajoz",
	"07": "Illes Balears",
	"08": "Barcelona",
	"09": "Burgos",
	"10": "Cáceres",
	"11": "Cádiz",
	"12": "Castelló/Castellón",
	"13": "Ciudad Real",
	"14": "Córdoba",
	"15": "A Coruña",
	"16": "Cuenca",
	"17": "Girona",
	"18": "Granada",
	"19": "Guadalajara",
	"20": "Gipuzkoa",
	"21": "Huelva",
	"22": "Huesca",
	"23": "Jaén",
	"24": "León",
	"25": "Lleida",
	"26": "La Rioja",
	"27": "Lugo",
	"28": "Madrid",
	"29": "Málaga",
	"30": "Murcia",
	"31": "Navarra",
	"32": "Ourense",
	"33": "Asturias",
	"34": "Palencia",
	"35": "Las Palmas",
	"36": "Pontevedra",
	"37": "Salamanca",
	"38": "Santa Cruz de Tenerife",
	"39": "Cantabria",
	"40": "Segovia",
	"41": "Sevilla",
	"42": "Soria",
	"43": "Tarragona",
	"44": "Teruel",
	"45": "Toledo",
	"46": "València/Valencia",
	"47": "Valladolid",
	"48": "Bizkaia",
	"49": "Zamora",
	"50": "Zaragoza",
	"51": "Ceuta",
	"52": "Melilla",
}

// ProvinceFromINE resolves a two-digit INE province code to the province
// name. The radar feed publishes the code without a fixed width
// (Almería arrives as "4", docs/feeds.md §6), so a single digit is
// zero-padded before the lookup and the input is trimmed. An unknown code
// returns false: the caller must serialise province as null, never a guess.
func ProvinceFromINE(code string) (string, bool) {
	c := strings.TrimSpace(code)
	if len(c) == 1 {
		c = "0" + c
	}
	name, ok := provinceINE[c]
	return name, ok
}
