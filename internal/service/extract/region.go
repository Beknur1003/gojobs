package extract

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/Beknur1003/gojobs/internal/models"
)

// regionOrder is the order regions are listed in, here and on the site.
var regionOrder = []models.Region{
	models.RegionWorld, models.RegionKZ, models.RegionCIS, models.RegionEurope,
	models.RegionNA, models.RegionLatAm, models.RegionAsia,
}

// placeNames pin a vacancy to a region: countries, the cities employers list,
// macro-regions. Lowercase regex fragments matched as whole words; Russian
// names are stems, since they decline ("в Москве", "из Казахстана"). Names
// of different places at once (Georgia, Cambridge, Birmingham) are left out:
// a wrong region is worse than none.
var placeNames = map[models.Region][]string{
	models.RegionKZ: {
		`kazakhstan`, `almaty`, `astana`, `nur-sultan`, `shymkent`, `karaganda`, `aktobe`, `atyrau`, `aktau`, `pavlodar`, `kz`,
		`казахстан\p{L}*`, `алматы`, `алма-ат\p{L}*`, `астан[аеуы]`, `шымкент\p{L}*`, `караганд\p{L}*`, `актобе`, `атырау`,
		`актау`, `павлодар\p{L}*`, `усть-каменогорск\p{L}*`,
	},
	models.RegionCIS: {
		`russia`, `russian federation`, `moscow`, `saint petersburg`, `st\.? petersburg`, `novosibirsk`, `yekaterinburg`,
		`ekaterinburg`, `kazan`, `nizhny novgorod`, `krasnodar`, `innopolis`, `belarus`, `minsk`, `uzbekistan`, `tashkent`,
		`kyrgyzstan`, `bishkek`, `armenia`, `yerevan`, `tbilisi`, `batumi`, `azerbaijan`, `baku`, `tajikistan`, `moldova`,
		`chisinau`, `cis`,
		`росси\p{L}*`, `рф`, `снг`, `москв\p{L}*`, `мск`, `санкт-петербург\p{L}*`, `петербург\p{L}*`, `спб`, `питер\p{L}*`,
		`новосибирск\p{L}*`, `екатеринбург\p{L}*`, `казан[ьи]`, `нижн\p{L}* новгород\p{L}*`, `краснодар\p{L}*`, `самар[аеуы]`,
		`иннополис\p{L}*`, `перм[ьи]`, `томск\p{L}*`, `омск\p{L}*`, `воронеж\p{L}*`, `уф[аеуы]`, `челябинск\p{L}*`,
		`красноярск\p{L}*`, `ростов\p{L}*`, `сочи`, `калининград\p{L}*`, `беларус\p{L}*`, `белорус\p{L}*`, `рб`, `минск\p{L}*`,
		`узбекистан\p{L}*`, `ташкент\p{L}*`, `кыргызстан\p{L}*`, `киргиз\p{L}*`, `бишкек\p{L}*`, `армени[яиюей]`, `ереван\p{L}*`,
		`грузи[яиюей]`, `тбилиси`, `батуми`, `азербайджан\p{L}*`, `баку`, `таджикистан\p{L}*`, `молдов\p{L}*`,
	},
	models.RegionEurope: {
		`europe`, `european union`, `emea`, `eea`, `dach`, `nordics`, `benelux`, `cet`, `cest`, `uk`, `united kingdom`,
		`great britain`, `britain`, `england`, `scotland`, `wales`, `ireland`, `germany`, `deutschland`, `france`,
		`netherlands`, `holland`, `belgium`, `luxembourg`, `switzerland`, `austria`, `spain`, `españa`, `portugal`, `italy`,
		`poland`, `czech republic`, `czechia`, `slovakia`, `hungary`, `romania`, `bulgaria`, `greece`, `croatia`, `serbia`,
		`montenegro`, `slovenia`, `bosnia`, `macedonia`, `albania`, `kosovo`, `cyprus`, `malta`, `estonia`, `latvia`,
		`lithuania`, `finland`, `sweden`, `norway`, `denmark`, `iceland`, `ukraine`, `turkey`, `türkiye`,
		`london`, `manchester`, `edinburgh`, `glasgow`, `bristol`, `leeds`, `belfast`, `oxford`, `dublin`, `cork`, `berlin`,
		`munich`, `münchen`, `hamburg`, `frankfurt`, `cologne`, `köln`, `stuttgart`, `düsseldorf`, `dusseldorf`, `leipzig`,
		`dresden`, `karlsruhe`, `paris`, `lyon`, `toulouse`, `nantes`, `bordeaux`, `lille`, `amsterdam`, `rotterdam`,
		`the hague`, `utrecht`, `eindhoven`, `brussels`, `antwerp`, `ghent`, `zurich`, `zürich`, `geneva`, `basel`,
		`lausanne`, `bern`, `vienna`, `madrid`, `barcelona`, `valencia`, `malaga`, `málaga`, `seville`, `bilbao`, `lisbon`,
		`porto`, `braga`, `milan`, `milano`, `rome`, `turin`, `bologna`, `warsaw`, `krakow`, `kraków`, `cracow`, `wroclaw`,
		`wrocław`, `gdansk`, `gdańsk`, `poznan`, `poznań`, `lodz`, `łódź`, `katowice`, `prague`, `brno`, `bratislava`,
		`budapest`, `bucharest`, `cluj(?:-napoca)?`, `iasi`, `sofia`, `plovdiv`, `athens`, `thessaloniki`, `zagreb`,
		`belgrade`, `novi sad`, `podgorica`, `ljubljana`, `sarajevo`, `skopje`, `tirana`, `limassol`, `nicosia`, `larnaca`,
		`paphos`, `valletta`, `tallinn`, `riga`, `vilnius`, `kaunas`, `helsinki`, `espoo`, `tampere`, `stockholm`,
		`gothenburg`, `göteborg`, `malmö`, `malmo`, `oslo`, `copenhagen`, `aarhus`, `reykjavik`, `kyiv`, `kiev`, `lviv`,
		`kharkiv`, `odesa`, `odessa`, `dnipro`, `istanbul`, `ankara`, `izmir`, `antalya`,
		`европ\p{L}*`, `евросоюз\p{L}*`, `великобритани\p{L}*`, `англи[яиюей]`, `лондон\p{L}*`, `германи\p{L}*`, `берлин\p{L}*`,
		`мюнхен\p{L}*`, `франци\p{L}*`, `париж\p{L}*`, `нидерланд\p{L}*`, `голланди\p{L}*`, `амстердам\p{L}*`, `испани\p{L}*`,
		`мадрид\p{L}*`, `барселон\p{L}*`, `португали\p{L}*`, `лиссабон\p{L}*`, `итали[яиюей]`, `польш\p{L}*`, `варшав\p{L}*`,
		`чехи[яиюей]`, `праг[аеиу]`, `серби[яиюей]`, `белград\p{L}*`, `черногори\p{L}*`, `кипр\p{L}*`, `лимассол\p{L}*`,
		`мальт[аеуы]`, `эстони\p{L}*`, `таллин\p{L}*`, `латви\p{L}*`, `риг[аеиу]`, `литв[аеуы]`, `вильнюс\p{L}*`,
		`финлянди\p{L}*`, `хельсинки`, `швеци\p{L}*`, `стокгольм\p{L}*`, `норвеги\p{L}*`, `дани[яиюей]`, `украин\p{L}*`,
		`киев\p{L}*`, `турци\p{L}*`, `стамбул\p{L}*`, `анталь\p{L}*`, `австри\p{L}*`, `швейцари\p{L}*`, `бельги\p{L}*`,
		`ирланди\p{L}*`, `болгари\p{L}*`, `румыни\p{L}*`, `венгри\p{L}*`, `словаки\p{L}*`, `хорвати\p{L}*`, `словени\p{L}*`,
		`греци\p{L}*`, `албани\p{L}*`, `македони\p{L}*`, `босни\p{L}*`,
	},
	models.RegionNA: {
		`united states`, `usa`, `u\.s\.(?:a\.)?`, `north america`, `america`, `americas`, `canada`, `california`,
		`new york`, `texas`, `washington`, `massachusetts`, `illinois`, `colorado`, `oregon`, `florida`, `virginia`,
		`north carolina`, `south carolina`, `utah`, `arizona`, `pennsylvania`, `new jersey`, `michigan`, `ohio`, `minnesota`,
		`maryland`, `tennessee`, `missouri`, `wisconsin`, `indiana`, `connecticut`, `nevada`, `kentucky`, `louisiana`,
		`oklahoma`, `kansas`, `iowa`, `alabama`, `idaho`, `nebraska`, `new hampshire`, `rhode island`, `delaware`, `vermont`,
		`maine`, `montana`, `wyoming`, `alaska`, `hawaii`, `new mexico`, `arkansas`, `mississippi`, `district of columbia`,
		`ontario`, `quebec`, `québec`, `british columbia`, `alberta`, `manitoba`, `nova scotia`, `saskatchewan`,
		`san francisco`, `sf`, `bay area`, `silicon valley`, `nyc`, `brooklyn`, `manhattan`, `seattle`, `austin`, `boston`,
		`chicago`, `los angeles`, `san diego`, `san jose`, `mountain view`, `sunnyvale`, `palo alto`, `menlo park`,
		`santa clara`, `cupertino`, `redwood city`, `san mateo`, `oakland`, `berkeley`, `emeryville`, `foster city`,
		`pleasanton`, `irvine`, `santa monica`, `orange county`, `denver`, `boulder`, `portland`, `atlanta`, `miami`, `dallas`, `houston`,
		`salt lake city`, `mclean`, `reston`, `herndon`, `arlington`, `raleigh`, `durham`, `pittsburgh`,
		`philadelphia`, `detroit`, `minneapolis`, `nashville`, `columbus`, `kansas city`, `st\.? louis`, `las vegas`,
		`bellevue`, `redmond`, `kirkland`, `sacramento`, `charlotte`, `tampa`, `orlando`, `baltimore`, `jersey city`,
		`hoboken`, `stamford`, `lehi`, `provo`, `boise`, `bastrop`, `chantilly`, `ann arbor`, `scottsdale`,
		`cincinnati`, `cleveland`, `indianapolis`, `milwaukee`, `toronto`, `vancouver`, `montreal`, `montréal`,
		`ottawa`, `calgary`, `waterloo`, `kitchener`, `edmonton`, `winnipeg`, `halifax`, `mississauga`,
		`сша`, `америк[аеиу]`, `канад\p{L}*`, `нью-йорк\p{L}*`, `сан-франциско`, `калифорни\p{L}*`, `торонто`,
	},
	models.RegionLatAm: {
		`latam`, `americas`, `mexico`, `méxico`, `brazil`, `brasil`, `argentina`, `chile`, `colombia`, `peru`, `perú`,
		`uruguay`, `costa rica`, `ecuador`, `guatemala`, `panama`, `panamá`, `puerto rico`, `venezuela`, `bolivia`,
		`paraguay`, `dominican republic`, `el salvador`, `honduras`, `nicaragua`, `mexico city`, `ciudad de méxico`, `cdmx`,
		`guadalajara`, `monterrey`, `são paulo`, `sao paulo`, `rio de janeiro`, `florianópolis`, `florianopolis`,
		`belo horizonte`, `curitiba`, `recife`, `buenos aires`, `santiago`, `bogotá`, `bogota`, `medellín`, `medellin`,
		`lima`, `montevideo`, `san josé`, `san juan`, `quito`,
		`латам`, `мексик\p{L}*`, `бразили\p{L}*`, `аргентин\p{L}*`, `чили`, `колумби\p{L}*`, `уругва\p{L}*`,
	},
	models.RegionAsia: {
		`asia`, `apac`, `asia pacific`, `anz`, `india`, `singapore`, `japan`, `china`, `taiwan`, `korea`, `vietnam`,
		`viet nam`, `thailand`, `malaysia`, `indonesia`, `philippines`, `pakistan`, `bangladesh`, `sri lanka`, `nepal`,
		`uae`, `united arab emirates`, `saudi arabia`, `qatar`, `bahrain`, `kuwait`, `oman`, `israel`, `australia`,
		`new zealand`, `bangalore`, `bengaluru`, `hyderabad`, `pune`, `chennai`, `mumbai`, `delhi`, `gurgaon`, `gurugram`,
		`noida`, `kolkata`, `ahmedabad`, `kochi`, `jaipur`, `coimbatore`, `tokyo`, `osaka`, `beijing`, `shanghai`,
		`shenzhen`, `hangzhou`, `guangzhou`, `dalian`, `chengdu`, `wuhan`, `nanjing`, `suzhou`, `hong kong`, `taipei`, `seoul`, `ho chi minh`, `hcmc`, `saigon`, `hanoi`,
		`da nang`, `bangkok`, `kuala lumpur`, `petaling jaya`, `penang`, `jakarta`, `bali`, `manila`, `makati`, `cebu`,
		`karachi`, `lahore`, `islamabad`, `dhaka`, `colombo`, `kathmandu`, `dubai`, `abu dhabi`, `riyadh`, `doha`,
		`tel aviv`, `jerusalem`, `haifa`, `herzliya`, `sydney`, `melbourne`, `brisbane`, `perth`, `adelaide`, `canberra`,
		`auckland`, `wellington`,
		`ази[яиюей]`, `инди[яиюей]`, `сингапур\p{L}*`, `япони\p{L}*`, `кита[йея]`, `гонконг\p{L}*`, `тайван\p{L}*`,
		`коре[яиюей]`, `вьетнам\p{L}*`, `таиланд\p{L}*`, `тайланд\p{L}*`, `малайзи\p{L}*`, `индонези\p{L}*`, `бали`,
		`филиппин\p{L}*`, `оаэ`, `эмират\p{L}*`, `дуба[йеяю]`, `абу-даби`, `израил\p{L}*`, `тель-авив\p{L}*`,
		`австрали\p{L}*`, `катар\p{L}*`, `пхукет\p{L}*`, `бангкок\p{L}*`,
	},
}

// placeMasks rewrite names that contain another region's name before
// matching: "Latin America" is not the USA, "Porto Alegre" is not Portugal.
var placeMasks = strings.NewReplacer(
	"latin america", "latam", "south america", "latam", "central america", "latam",
	"латинская америка", "латам", "латинской америке", "латам", "латинской америки", "латам",
	"new mexico", "usa", "new south wales", "australia", "porto alegre", "brasil",
)

// notPlaces are places a post rules out: "Remote (вне РФ)" is not Russia.
// Lowercase.
var notPlaces = regexp.MustCompile(`(?:вне|кроме|не из|не в|за пределами|исключая)\s+(?:рф|росси\p{L}*|снг|беларус\p{L}*|рб)|(?:outside|except|excluding|not in)\s+(?:of\s+)?(?:the\s+)?(?:russia|belarus|rf|cis)`)

var placeRe = func() map[models.Region]*regexp.Regexp {
	out := map[models.Region]*regexp.Regexp{}
	for r, names := range placeNames {
		out[r] = regexp.MustCompile(`(?:^|[^\p{L}\p{N}])(?:` + strings.Join(names, "|") + `)(?:[^\p{L}\p{N}]|$)`)
	}
	return out
}()

// codeRegions maps two-letter country codes, as company boards write them at
// the end of a location ("Petaling Jaya, my"), to regions.
var codeRegions = map[string]models.Region{
	"kz": models.RegionKZ,
	"ru": models.RegionCIS, "by": models.RegionCIS, "uz": models.RegionCIS, "kg": models.RegionCIS, "am": models.RegionCIS,
	"ge": models.RegionCIS, "az": models.RegionCIS, "tj": models.RegionCIS, "md": models.RegionCIS,
	"eu": models.RegionEurope, "uk": models.RegionEurope, "gb": models.RegionEurope, "ie": models.RegionEurope,
	"de": models.RegionEurope, "fr": models.RegionEurope, "nl": models.RegionEurope, "be": models.RegionEurope,
	"lu": models.RegionEurope, "ch": models.RegionEurope, "at": models.RegionEurope, "es": models.RegionEurope,
	"pt": models.RegionEurope, "it": models.RegionEurope, "pl": models.RegionEurope, "cz": models.RegionEurope,
	"sk": models.RegionEurope, "hu": models.RegionEurope, "ro": models.RegionEurope, "bg": models.RegionEurope,
	"gr": models.RegionEurope, "hr": models.RegionEurope, "rs": models.RegionEurope, "si": models.RegionEurope,
	"ee": models.RegionEurope, "lv": models.RegionEurope, "lt": models.RegionEurope, "fi": models.RegionEurope,
	"se": models.RegionEurope, "no": models.RegionEurope, "dk": models.RegionEurope, "is": models.RegionEurope,
	"ua": models.RegionEurope, "tr": models.RegionEurope, "cy": models.RegionEurope, "mt": models.RegionEurope,
	"us": models.RegionNA, "ca": models.RegionNA,
	"mx": models.RegionLatAm, "br": models.RegionLatAm, "ar": models.RegionLatAm, "cl": models.RegionLatAm,
	"co": models.RegionLatAm, "pe": models.RegionLatAm, "uy": models.RegionLatAm, "cr": models.RegionLatAm,
	"in": models.RegionAsia, "sg": models.RegionAsia, "jp": models.RegionAsia, "cn": models.RegionAsia,
	"hk": models.RegionAsia, "tw": models.RegionAsia, "kr": models.RegionAsia, "vn": models.RegionAsia,
	"th": models.RegionAsia, "my": models.RegionAsia, "id": models.RegionAsia, "ph": models.RegionAsia,
	"pk": models.RegionAsia, "bd": models.RegionAsia, "lk": models.RegionAsia, "ae": models.RegionAsia,
	"sa": models.RegionAsia, "qa": models.RegionAsia, "il": models.RegionAsia, "au": models.RegionAsia,
	"nz": models.RegionAsia,
}

// stateCodes are US states and Canadian provinces. "San Jose, CA" is
// California, and "CO", "DE" or "IN" after a US city is a state, not a country.
var stateCodes = func() map[string]bool {
	out := map[string]bool{}
	for _, c := range strings.Fields(`al ak az ar ca co ct de fl ga hi id il in ia ks ky la me md ma mi mn ms mo mt ne nv
		nh nj nm ny nc nd oh ok or pa ri sc sd tn tx ut vt va wa wv wi wy dc on bc qc ab mb ns nb nl pe sk`) {
		out[c] = true
	}
	return out
}()

// currencyRegions: a salary paid in rubles or tenge places the role even when
// the post names no city.
var currencyRegions = map[string]models.Region{
	"KZT": models.RegionKZ,
	"RUB": models.RegionCIS, "BYN": models.RegionCIS, "UZS": models.RegionCIS, "KGS": models.RegionCIS,
	"AMD": models.RegionCIS, "GEL": models.RegionCIS, "AZN": models.RegionCIS,
	"EUR": models.RegionEurope, "GBP": models.RegionEurope, "PLN": models.RegionEurope, "CHF": models.RegionEurope,
	"SEK": models.RegionEurope, "NOK": models.RegionEurope, "DKK": models.RegionEurope, "CZK": models.RegionEurope,
	"HUF": models.RegionEurope, "RON": models.RegionEurope, "UAH": models.RegionEurope, "TRY": models.RegionEurope,
	"RSD": models.RegionEurope,
	"CAD": models.RegionNA,
	"BRL": models.RegionLatAm, "MXN": models.RegionLatAm, "ARS": models.RegionLatAm, "CLP": models.RegionLatAm,
	"COP": models.RegionLatAm, "PEN": models.RegionLatAm,
	"INR": models.RegionAsia, "SGD": models.RegionAsia, "JPY": models.RegionAsia, "AUD": models.RegionAsia,
	"NZD": models.RegionAsia, "HKD": models.RegionAsia, "CNY": models.RegionAsia, "KRW": models.RegionAsia,
	"VND": models.RegionAsia, "THB": models.RegionAsia, "MYR": models.RegionAsia, "IDR": models.RegionAsia,
	"AED": models.RegionAsia, "ILS": models.RegionAsia, "BDT": models.RegionAsia, "PKR": models.RegionAsia,
}

var (
	// Region abbreviations that are only safe in their original capitals:
	// "US" is the country, "us" is a pronoun.
	upperPlaces = regexp.MustCompile(`\b(?:US|USA|UK|EU|EMEA|APAC|LATAM|CET|CEST)\b|\bU\.S\.`)
	upperRegion = map[string]models.Region{
		"US": models.RegionNA, "USA": models.RegionNA, "U.S.": models.RegionNA, "UK": models.RegionEurope,
		"EU": models.RegionEurope, "EMEA": models.RegionEurope, "APAC": models.RegionAsia, "LATAM": models.RegionLatAm,
		"CET": models.RegionEurope, "CEST": models.RegionEurope,
	}

	// A location that means "anywhere". Lowercase.
	worldPlace = regexp.MustCompile(`worldwide|world-wide|anywhere|global(?:ly)?|international|any (?:location|country)|весь мир|по всему миру|любая страна|из любой (?:точки|страны)`)
	// The same stated in a description. Only explicit statements count:
	// "work from anywhere" is usually a perk, "remote, global culture" is
	// culture, "regardless of location" is about the salary range. Lowercase.
	worldText = regexp.MustCompile(`remote\s*[(\-–—,/:|]\s*(?:worldwide|global(?:ly)?|anywhere)\s*(?:[)\n,.;|•]|$)|location\s*:\s*(?:anywhere|worldwide|global)|we hire (?:globally|worldwide|anywhere)|candidates (?:from )?(?:anywhere|worldwide|globally)|из любой (?:точки мира|страны)|(?:гео)?локаци[яи] не важн`)
	// What may follow a "worldwide" and take it back: "anywhere in the US",
	// "из любой точки России". Lowercase.
	worldLimit = regexp.MustCompile(`^\s*(?:in|within|across|inside|for|up to)\b|^\s*(?:в|по|на)\s|^\s*\d`)
	worldKeep  = regexp.MustCompile(`^\s*in the world\b`) // "anywhere in the world"

	// "must be based in Canada": a restriction stated in the description.
	basedIn = regexp.MustCompile(`(?:based|located|living|residing|reside|live)\s+(?:in|within)\s+(?:the\s+)?([^.;:!?\n]{2,60})`)

	// Signs of a Russian or Kazakh employer anywhere in a post: pay in rubles
	// or tenge, a legal form, the labour code. Signs stick to numbers
	// ("300 000₽"), so they have no word edges. Lowercase.
	cisMarks = regexp.MustCompile(`₽|(?:^|[^\p{L}])(?:руб|рублей|рубля|тк рф|ооо|аккредитованн\p{L}*)(?:[^\p{L}]|$)`)
	kzMarks  = regexp.MustCompile(`₸|(?:^|[^\p{L}])(?:тенге|тоо)(?:[^\p{L}]|$)`)
)

// Regions reads where a vacancy can be worked from: the location the source
// gave and two-letter codes in it, the title, the salary currency; when none
// of them names a place, the top of the post and the script it is written
// in. A remote role whose location names no country but says "worldwide"
// also gets RegionWorld. Kazakhstan counts as the CIS too.
func Regions(j models.Job) []models.Region {
	found := map[models.Region]bool{}
	add := func(rs map[models.Region]bool) {
		for r := range rs {
			found[r] = true
		}
	}

	loc := strings.TrimSpace(j.Location)
	names := placesIn(loc)
	add(names)
	add(codesIn(loc, len(names) > 0))
	add(upperIn(loc))
	add(placesIn(j.Title))
	add(upperIn(j.Title))

	// "Worldwide" only when the location and title name no country: boards
	// write "Remote - International" over a role that is for Spain.
	text := strings.ToLower(j.Text)
	if !placed(found) {
		remote := false
		for _, f := range j.Formats {
			remote = remote || f == models.FormatRemote
		}
		if anyWorld(worldPlace, strings.ToLower(loc)) || remote && anyWorld(worldText, text) {
			found[models.RegionWorld] = true
		}
	}

	if r, ok := currencyRegions[j.Salary.Currency]; ok {
		found[r] = true
	}
	// The marks are Cyrillic or currency signs: skip the regexps over long
	// English descriptions that cannot contain them.
	if (j.Lang == "ru" || strings.ContainsRune(text, '₽')) && cisMarks.MatchString(text) {
		found[models.RegionCIS] = true
	}
	if (j.Lang == "ru" || strings.ContainsRune(text, '₸')) && kzMarks.MatchString(text) {
		found[models.RegionKZ] = true
	}

	// Nothing yet ("Remote", "Hybrid", no location at all): the top of the
	// post usually says ("Офис (Астана)", "Remote (US)"), and so does the
	// script it is written in.
	if !placed(found) {
		head := firstRunes(j.Text, 700)
		add(placesIn(head))
		add(upperIn(head))
		for _, m := range basedIn.FindAllStringSubmatch(text, -1) {
			add(placesIn(m[1]))
		}
	}
	if !placed(found) {
		if r, ok := scriptRegion(j.Text); ok {
			found[r] = true
		} else if r, ok := sourceRegion(j); ok {
			found[r] = true
		} else if j.Lang == "ru" {
			// A Russian post that names no place is, nearly always, a Russian
			// or CIS employer: international ones say where they are.
			found[models.RegionCIS] = true
		}
	}

	if found[models.RegionKZ] {
		found[models.RegionCIS] = true
	}
	var out []models.Region
	for _, r := range regionOrder {
		if found[r] {
			out = append(out, r)
		}
	}
	return out
}

// sourceRegions is where a board's roles are when a post names no place:
// Djinni is a Ukrainian board.
var sourceRegions = map[string]models.Region{"djinni": models.RegionEurope}

func sourceRegion(j models.Job) (models.Region, bool) {
	if len(j.Sources) == 0 {
		return "", false
	}
	r, ok := sourceRegions[j.Sources[0].Source]
	return r, ok
}

// upperIn returns the regions named by capitalized abbreviations in s.
func upperIn(s string) map[models.Region]bool {
	out := map[models.Region]bool{}
	for _, m := range upperPlaces.FindAllString(s, -1) {
		out[upperRegion[m]] = true
	}
	return out
}

// scriptRegion places a post by its letters: Kazakh letters are Kazakhstan,
// Ukrainian ones Europe; Chinese, Japanese, Korean or Thai text is Asia;
// Polish, Czech or German letters are Europe.
func scriptRegion(text string) (models.Region, bool) {
	asian, european, kazakh, ukrainian := 0, 0, 0, 0
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana, unicode.Thai):
			asian++
		case strings.ContainsRune("ąęłśżźćńřěůčšžőűäöüß", unicode.ToLower(r)):
			european++
		case strings.ContainsRune("әғқңөұүһ", unicode.ToLower(r)):
			kazakh++
		case strings.ContainsRune("їєґ", unicode.ToLower(r)):
			ukrainian++
		}
	}
	switch {
	case kazakh >= 5:
		return models.RegionKZ, true
	case ukrainian >= 5:
		return models.RegionEurope, true
	case asian >= 10:
		return models.RegionAsia, true
	case european >= 10:
		return models.RegionEurope, true
	}
	return "", false
}

// placed reports whether any region other than "world" is known.
func placed(found map[models.Region]bool) bool {
	for r := range found {
		if r != models.RegionWorld {
			return true
		}
	}
	return false
}

// placesIn returns the regions whose place names occur in s.
func placesIn(s string) map[models.Region]bool {
	out := map[models.Region]bool{}
	if s == "" {
		return out
	}
	s = placeMasks.Replace(strings.ToLower(s))
	if strings.Contains(s, "вне") || strings.Contains(s, "кроме") || strings.Contains(s, "не ") ||
		strings.Contains(s, "за пределами") || strings.Contains(s, "исключая") || strings.Contains(s, "outside") ||
		strings.Contains(s, "except") || strings.Contains(s, "excluding") || strings.Contains(s, "not in") {
		s = notPlaces.ReplaceAllString(s, " ")
	}
	for r, re := range placeRe {
		if re.MatchString(s) {
			out[r] = true
		}
	}
	return out
}

// codesIn reads two-letter codes in a location. Codes in capitals are a US
// state or a country ("San Jose, CA", "Remote, US"); a lowercase code counts
// only at the end, where boards put the country ("Bengaluru, KA, in"). A code
// that is both a state and a country ("CO", "IN") counts as a state only when
// no place name settled the location already.
func codesIn(loc string, named bool) map[models.Region]bool {
	out := map[models.Region]bool{}
	tokens := strings.FieldsFunc(loc, func(r rune) bool { return !unicode.IsLetter(r) })
	for i, t := range tokens {
		if len(t) != 2 || t[0] > unicode.MaxASCII || t[1] > unicode.MaxASCII {
			continue
		}
		code := strings.ToLower(t)
		switch {
		case t == strings.ToUpper(t):
			region, isCountry := codeRegions[code]
			switch {
			case stateCodes[code] && (!isCountry || !named):
				out[models.RegionNA] = true
			case isCountry && !stateCodes[code]:
				out[region] = true
			}
		case t == code && i == len(tokens)-1 && i > 0:
			if region, ok := codeRegions[code]; ok {
				out[region] = true
			}
		}
	}
	return out
}

// anyWorld reports a "worldwide" match that the words right after it do not
// limit: "anywhere in the US", "из любой точки России".
func anyWorld(re *regexp.Regexp, lower string) bool {
	for _, m := range re.FindAllStringIndex(lower, -1) {
		after := lower[m[1]:min(len(lower), m[1]+24)]
		if worldKeep.MatchString(after) {
			return true
		}
		if worldLimit.MatchString(after) || placed(placesIn(after)) {
			continue
		}
		return true
	}
	return false
}
