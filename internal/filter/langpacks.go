package filter

// LanguagePack is the modular, data-driven language definition used by Tier-0.
// Adding a language never requires branching in scoring code: register another
// pack here and the detector/scorer/stemmer pick it up automatically.
type LanguagePack struct {
	Code string
	Name string

	// DetectWords are high-signal function/intent words for language detection.
	DetectWords []string
	// StopWords are common function words retained for token normalization and
	// diagnostics (they are not removed from BM25, to keep tag phrases intact).
	StopWords []string

	IntentSignals  []string
	DIYSignals     []string
	SellerSignals  []string
	HRSignals      []string
	SpamSignals    []string
	ServiceSignals []string
}

var languagePacks = []LanguagePack{
	{
		Code: "en",
		Name: "English",
		DetectWords: []string{
			"the", "and", "for", "with", "you", "your", "are", "is", "looking", "need",
			"anyone", "someone", "hello", "hi", "can", "help", "please", "want", "hire",
			"budget", "website", "developer", "searching", "recommend",
		},
		StopWords: []string{
			"the", "a", "an", "and", "or", "but", "if", "then", "than", "that", "this", "these", "those",
			"is", "are", "was", "were", "be", "been", "being", "am", "i", "you", "he", "she", "it", "we", "they",
			"me", "my", "your", "his", "her", "its", "our", "their", "to", "of", "in", "on", "at", "for", "with",
			"from", "by", "as", "about", "into", "over", "after", "before", "not", "no", "yes", "do", "does", "did",
			"have", "has", "had", "can", "could", "will", "would", "should", "may", "might", "must", "just", "very",
			"please", "thanks", "thank", "hello", "hi",
		},
		IntentSignals: []string{
			"looking for", "need", "need help", "need someone", "who can", "can anyone", "can someone",
			"recommend", "recommendations", "any recommendations", "searching for", "want to hire",
			"hire someone", "hire a", "freelancer", "agency", "quote", "quote me", "budget",
			"willing to pay", "help me with", "anyone know", "iso", "seeking", "in need of",
			"where can i find", "do you know anyone", "anyone available", "looking to hire",
		},
		DIYSignals: []string{
			"i tried", "tried everything", "can't figure out", "cannot figure out", "struggling with",
			"no idea how", "spent hours", "keep failing", "give up", "do it myself", "diy",
			"fix it myself", "frustrated with", "not working", "wasted hours", "tried myself",
			"can't get it to work", "nothing works",
		},
		SellerSignals: []string{
			"i offer", "i provide", "my services", "our services", "dm me", "pm me", "message me",
			"portfolio", "my portfolio", "hire me", "i can build", "i can do", "we build", "we offer",
			"price list", "rates", "cases", "testimonials", "guaranteed results", "best price",
			"cheap", "affordable", "special offer", "contact me for", "i specialize in",
		},
		HRSignals: []string{
			"hiring", "vacancy", "job opening", "full-time", "full time", "part-time", "part time",
			"salary", "resume", "cv", "apply now", "send your cv", "send your resume", "recruiter",
			"position", "we are looking for a developer", "internship", "remote job", "join our team",
			"compensation", "benefits package", "hr department",
		},
		SpamSignals: []string{
			"crypto", "cryptocurrency", "p2p", "usdt", "airdrop", "investment", "guaranteed profit",
			"passive income", "binary options", "forex signals", "earn money fast", "giveaway",
			"telegram stars", "buy followers", "casino", "betting", "presale", "nft drop",
			"referral bonus", "double your", "risk-free profit", "financial freedom",
		},
		ServiceSignals: []string{
			"captcha", "verify you are human", "verification required", "welcome to the group",
			"read the rules", "joined the group", "left the group", "pinned message",
			"subscribe to the channel", "automated message", "you have been muted",
			"you have been banned", "warning issued", "anti-spam bot",
		},
	},
	{
		Code: "ru",
		Name: "Russian",
		DetectWords: []string{
			"ищу", "нужен", "нужна", "нужно", "кто", "что", "это", "как", "для", "мне",
			"очень", "подскажите", "посоветуйте", "бюджет", "есть", "может", "помогите",
			"сделать", "сайт", "разработчик", "привет", "спасибо", "пожалуйста",
		},
		StopWords: []string{
			"и", "в", "на", "не", "что", "с", "по", "это", "как", "так", "но", "а", "или", "же", "бы",
			"для", "от", "до", "из", "у", "о", "об", "при", "над", "под", "за", "мне", "мой", "моя", "мы",
			"вы", "ты", "он", "она", "они", "всё", "все", "есть", "был", "была", "быть", "можно", "очень",
			"просто", "тоже", "уже", "ещё", "если", "когда", "где", "кто", "почему", "чтобы", "этот", "эта",
		},
		IntentSignals: []string{
			"ищу", "ищу специалиста", "ищу исполнителя", "ищу подрядчика", "ищем", "нужен", "нужна",
			"нужно", "нужен специалист", "кто может", "кто умеет", "кто сделает", "кто поможет",
			"подскажите", "посоветуйте", "посоветуйте специалиста", "требуется", "помогите найти",
			"есть кто", "может кто", "готов оплатить", "оплачу", "бюджет", "срочно нужен",
			"где найти", "порекомендуйте", "нужна помощь", "ищу человека",
		},
		DIYSignals: []string{
			"замучился", "не получается", "не могу настроить", "уже пробовал", "перепробовал",
			"своими руками", "сам пытался", "не разбираюсь", "руки опускаются", "устал бороться",
			"никак не выходит", "не работает", "потратил часы", "не выходит", "делал сам",
			"устал уже", "ничего не помогает",
		},
		SellerSignals: []string{
			"сделаю", "предлагаю услуги", "мои услуги", "наши услуги", "прайс", "прайс-лист",
			"кейсы", "портфолио", "обращайтесь", "пишите в личку", "пишите в лс", "недорого",
			"качественно и быстро", "берусь за", "опыт работы", "гарантия результата",
			"лучшая цена", "скидка", "специализируюсь на", "оказываю услуги",
		},
		HRSignals: []string{
			"вакансия", "вакансия:", "ищем сотрудника", "требуется разработчик", "требуется сотрудник",
			"резюме", "зарплата", "полная занятость", "удалённая работа", "удаленная работа",
			"трудоустройство", "отправьте резюме", "рекрутер", "стажировка", "в штат", "оклад",
			"join our team", "ищем в команду", "опыт от",
		},
		SpamSignals: []string{
			"криптовалюта", "крипта", "p2p", "usdt", "инвестиции", "гарантированный доход",
			"пассивный доход", "быстрый заработок", "биржа", "сигналы", "казино", "ставки",
			"розыгрыш", "звёзды телеграм", "звезды телеграм", "накрутка", "подписчики",
			"реферальная ссылка", "эйрдроп", "фонд", "трейдинг",
		},
		ServiceSignals: []string{
			"капча", "подтвердите, что вы не робот", "подтвердите что вы не робот", "добро пожаловать",
			"правила чата", "вступил в группу", "покинул группу", "закреплённое сообщение",
			"закрепленное сообщение", "подпишитесь на канал", "автоматическое сообщение",
			"забанен", "предупреждение", "анти-спам бот",
		},
	},
	{
		Code: "uk",
		Name: "Ukrainian",
		DetectWords: []string{
			"шукаю", "потрібен", "потрібна", "потрібно", "хто", "що", "це", "як", "для", "мені",
			"дуже", "підкажіть", "порадьте", "бюджет", "є", "може", "допоможіть", "зробити",
			"сайт", "розробник", "привіт", "дякую", "будь ласка", "щоб",
		},
		StopWords: []string{
			"і", "й", "та", "в", "у", "на", "не", "що", "з", "по", "це", "як", "так", "але", "або",
			"для", "від", "до", "із", "мені", "мій", "ми", "ви", "ти", "він", "вона", "вони", "все",
			"є", "був", "була", "бути", "можна", "дуже", "просто", "теж", "вже", "ще", "якщо", "коли",
			"де", "хто", "чому", "щоб",
		},
		IntentSignals: []string{
			"шукаю", "шукаю спеціаліста", "шукаю виконавця", "шукаю підрядника", "потрібен",
			"потрібна", "потрібно", "потрібен спеціаліст", "хто може", "хто вміє", "хто зробить",
			"хто допоможе", "підкажіть", "порадьте", "потрібно виконати", "допоможіть знайти",
			"чи є хтось", "готовий оплатити", "оплачу", "бюджет", "терміново потрібен",
			"де знайти", "порадьте спеціаліста", "потрібна допомога",
		},
		DIYSignals: []string{
			"замучився", "не виходить", "не можу налаштувати", "вже пробував", "перепробував",
			"своїми руками", "сам намагався", "не розбираюся", "втомився боротися",
			"ніяк не виходить", "не працює", "витратив години", "робив сам", "втомився",
			"нічого не допомагає",
		},
		SellerSignals: []string{
			"зроблю", "пропоную послуги", "мої послуги", "наші послуги", "прайс", "прайс-лист",
			"кейси", "портфоліо", "звертайтесь", "пишіть в особисті", "пишіть у лс", "недорого",
			"якісно і швидко", "беруся за", "досвід роботи", "гарантія результату",
			"найкраща ціна", "знижка", "спеціалізуюся на", "надаю послуги",
		},
		HRSignals: []string{
			"вакансія", "шукаємо співробітника", "потрібен розробник", "потрібен співробітник",
			"резюме", "зарплата", "повна зайнятість", "віддалена робота", "працевлаштування",
			"надішліть резюме", "рекрутер", "стажування", "в штат", "оклад", "у команду",
		},
		SpamSignals: []string{
			"криптовалюта", "крипта", "p2p", "usdt", "інвестиції", "гарантований дохід",
			"пасивний дохід", "швидкий заробіток", "біржа", "сигнали", "казино", "ставки",
			"розіграш", "зірки телеграм", "накрутка", "підписники",
			"реферальне посилання", "аірдроп", "трейдинг",
		},
		ServiceSignals: []string{
			"капча", "підтвердіть, що ви не робот", "підтвердіть що ви не робот", "ласкаво просимо",
			"правила чату", "вступив до групи", "покинув групу", "закріплене повідомлення",
			"підпишіться на канал", "автоматичне повідомлення", "забанено", "попередження",
			"анти-спам бот",
		},
	},
	{
		Code: "es",
		Name: "Spanish",
		DetectWords: []string{
			"busco", "necesito", "para", "que", "con", "alguien", "hola", "gracias", "presupuesto",
			"página", "pagina", "desarrollador", "quiero", "puede", "ayuda", "por", "favor", "mi",
			"un", "una", "estoy", "buscando",
		},
		StopWords: []string{
			"el", "la", "los", "las", "de", "del", "que", "y", "en", "un", "una", "por", "para", "con",
			"no", "se", "es", "al", "como", "más", "mas", "pero", "sus", "le", "ya", "muy", "sin",
			"sobre", "este", "esta", "esto", "cuando", "donde", "quien", "porque", "hola", "gracias",
		},
		IntentSignals: []string{
			"busco", "necesito", "estoy buscando", "alguien que", "quién puede", "quien puede",
			"recomiendan", "alguna recomendación", "alguna recomendacion", "necesito ayuda",
			"contratar", "presupuesto", "pagar", "dispuesto a pagar", "me recomiendan",
			"buscando especialista", "necesito un profesional", "saben de alguien",
			"dónde encuentro", "donde encuentro", "recomendaciones", "busco experto",
		},
		DIYSignals: []string{
			"ya intenté", "ya intente", "no puedo", "no consigo", "lo intenté todo", "lo intente todo",
			"estoy cansado de", "no sé cómo", "no se como", "horas intentando", "siempre falla",
			"hacerlo yo mismo", "por mi cuenta", "frustrado", "no funciona", "nada funciona",
		},
		SellerSignals: []string{
			"ofrezco", "mis servicios", "nuestros servicios", "mi portafolio", "contáctame",
			"contactame", "escríbeme", "escribeme", "te lo hago", "puedo hacerlo", "nosotros hacemos",
			"lista de precios", "tarifas", "casos de éxito", "garantizado", "mejor precio",
			"barato", "económico", "economico", "me especializo en",
		},
		HRSignals: []string{
			"vacante", "estamos contratando", "buscamos desarrollador", "currículum", "curriculum",
			"cv", "salario", "tiempo completo", "medio tiempo", "trabajo remoto", "envíe su cv",
			"envie su cv", "reclutador", "puesto", "prácticas", "practicas", "únete al equipo",
		},
		SpamSignals: []string{
			"criptomoneda", "cripto", "p2p", "usdt", "inversión", "inversion", "ganancias garantizadas",
			"ingreso pasivo", "gana dinero rápido", "gana dinero rapido", "forex", "señales", "senales",
			"casino", "apuestas", "sorteo", "estrellas de telegram", "seguidores", "referidos", "airdrop",
		},
		ServiceSignals: []string{
			"captcha", "verifica que no eres un robot", "bienvenido", "reglas del chat",
			"se unió al grupo", "se unio al grupo", "salió del grupo", "salio del grupo",
			"mensaje fijado", "suscríbete", "suscribete", "mensaje automático", "mensaje automatico",
			"advertido", "expulsado",
		},
	},
	{
		Code: "de",
		Name: "German",
		DetectWords: []string{
			"ich", "suche", "brauche", "für", "fur", "und", "jemand", "hallo", "danke", "budget",
			"webseite", "entwickler", "wer", "kann", "meine", "mit", "nicht", "bitte", "hilfe",
			"mein", "wir",
		},
		StopWords: []string{
			"der", "die", "das", "und", "in", "zu", "den", "von", "mit", "sich", "des", "auf", "für",
			"fur", "ist", "im", "dem", "nicht", "ein", "eine", "als", "auch", "es", "an", "werden",
			"aus", "er", "hat", "dass", "sie", "nach", "wird", "bei", "einer", "um", "am", "sind",
			"noch", "wie", "wenn", "aber", "oder", "ich", "du", "wir", "mein", "sehr", "bitte", "danke",
		},
		IntentSignals: []string{
			"suche", "brauche", "ich suche", "ich brauche", "wer kann", "kann jemand",
			"empfehlungen", "empfehlt mir", "hat jemand", "hilfe bei", "beauftragen", "budget",
			"bereit zu zahlen", "freiberufler", "agentur", "angebot", "suche experten",
			"kennt jemand", "wo finde ich", "suche unterstützung", "suche unterstutzung",
		},
		DIYSignals: []string{
			"schon versucht", "klappt nicht", "funktioniert nicht", "ich verzweifle", "keine ahnung wie",
			"stundenlang probiert", "ständig fehler", "standig fehler", "selbst machen",
			"alleine schaffe ich das nicht", "frustriert", "nichts funktioniert", "selbst probiert",
		},
		SellerSignals: []string{
			"ich biete", "meine dienstleistungen", "unsere dienstleistungen", "mein portfolio",
			"kontaktier mich", "kontaktiere mich", "schreib mir", "ich mache das", "wir machen",
			"preisliste", "stundensatz", "referenzen", "garantiert", "bester preis", "günstig",
			"gunstig", "spezialisiert auf", "ich übernehme", "ich ubernehme",
		},
		HRSignals: []string{
			"stellenangebot", "wir stellen ein", "wir suchen entwickler", "lebenslauf", "gehalt",
			"vollzeit", "teilzeit", "remote job", "bewerbung", "recruiter", "praktikum",
			"festanstellung", "bewerben sie sich", "arbeitsvertrag",
		},
		SpamSignals: []string{
			"kryptowährung", "kryptowahrung", "krypto", "p2p", "usdt", "investment",
			"garantierte gewinne", "passives einkommen", "schnell geld verdienen", "forex",
			"signale", "casino", "wetten", "gewinnspiel", "telegram stars", "follower kaufen",
			"referrals", "airdrop",
		},
		ServiceSignals: []string{
			"captcha", "bestätige dass du ein mensch bist", "bestatige dass du ein mensch bist",
			"willkommen", "chatregeln", "ist der gruppe beigetreten", "hat die gruppe verlassen",
			"angeheftete nachricht", "abonniere", "automatische nachricht",
			"verwarnt", "gesperrt",
		},
	},
	{
		Code: "fr",
		Name: "French",
		DetectWords: []string{
			"je", "cherche", "besoin", "pour", "et", "quelqu'un", "quelquun", "bonjour", "merci",
			"budget", "site", "développeur", "developpeur", "qui", "peut", "mon", "avec", "pas",
			"aide", "svp", "nous", "est",
		},
		StopWords: []string{
			"le", "la", "les", "de", "des", "du", "et", "en", "un", "une", "que", "qui", "dans",
			"pour", "pas", "sur", "au", "avec", "ce", "il", "elle", "ils", "nous", "vous", "je",
			"tu", "est", "sont", "mais", "ou", "très", "tres", "merci", "bonjour",
		},
		IntentSignals: []string{
			"je cherche", "je cherche quelqu'un", "besoin de", "j'ai besoin", "jai besoin",
			"qui peut", "quelqu'un peut", "quelquun peut", "recommande", "des recommandations",
			"quelqu'un connaît", "quelquun connait", "embaucher", "budget", "prêt à payer",
			"pret a payer", "freelance", "devis", "cherche un expert", "où trouver",
			"ou trouver", "connaissez-vous", "connaissez vous", "besoin d'aide", "besoin daide",
		},
		DIYSignals: []string{
			"déjà essayé", "deja essaye", "je n'arrive pas", "je narrive pas", "ça ne marche pas",
			"ca ne marche pas", "je galère", "je galere", "je ne sais pas comment", "des heures à essayer",
			"toujours des erreurs", "le faire moi-même", "le faire moi-meme", "tout seul", "frustré",
			"frustre", "rien ne marche",
		},
		SellerSignals: []string{
			"je propose", "mes services", "nos services", "mon portfolio", "contactez-moi",
			"contactez moi", "écris-moi", "ecris moi", "je peux le faire", "nous faisons",
			"liste de prix", "tarifs", "références", "references", "garanti", "meilleur prix",
			"pas cher", "abordable", "spécialisé en", "specialise en",
		},
		HRSignals: []string{
			"offre d'emploi", "offre demploi", "nous recrutons", "nous cherchons un développeur",
			"nous cherchons un developpeur", "cv", "salaire", "temps plein", "temps partiel",
			"télétravail", "teletravail", "envoyez votre cv", "recruteur", "stage", "cdi", "cdd",
			"rejoignez notre équipe", "rejoignez notre equipe",
		},
		SpamSignals: []string{
			"crypto", "cryptomonnaie", "p2p", "usdt", "investissement", "profits garantis",
			"revenu passif", "gagner de l'argent rapidement", "gagner de largent rapidement",
			"forex", "signaux", "casino", "paris", "tirage au sort", "telegram stars", "abonnés",
			"abonnes", "parrainage", "airdrop",
		},
		ServiceSignals: []string{
			"captcha", "vérifiez que vous êtes humain", "verifiez que vous etes humain",
			"bienvenue", "règles du chat", "regles du chat", "a rejoint le groupe",
			"a quitté le groupe", "a quitte le groupe", "message épinglé", "message epingle",
			"abonnez-vous", "abonnez vous", "message automatique", "averti", "banni",
		},
	},
	{
		Code: "pt",
		Name: "Portuguese",
		DetectWords: []string{
			"procuro", "preciso", "para", "que", "com", "alguém", "alguem", "olá", "ola",
			"obrigado", "orçamento", "orcamento", "site", "desenvolvedor", "quero", "pode",
			"ajuda", "meu", "não", "nao", "por", "favor", "estou",
		},
		StopWords: []string{
			"o", "a", "os", "as", "de", "do", "da", "dos", "das", "que", "e", "em", "um", "uma",
			"para", "com", "não", "nao", "se", "é", "e", "ao", "como", "mais", "mas", "seu", "sua",
			"eu", "você", "voce", "nós", "nos", "ele", "ela", "muito", "obrigado", "olá", "ola",
		},
		IntentSignals: []string{
			"procuro", "preciso", "estou procurando", "alguém que", "alguem que", "quem pode",
			"recomendam", "recomendações", "recomendacoes", "alguém conhece", "alguem conhece",
			"preciso de ajuda", "contratar", "orçamento", "orcamento", "disposto a pagar",
			"freelancer", "procuro especialista", "onde encontro", "conhece alguém",
			"conhece alguem", "preciso de um profissional",
		},
		DIYSignals: []string{
			"já tentei", "ja tentei", "não consigo", "nao consigo", "não funciona", "nao funciona",
			"estou cansado de", "não sei como", "nao sei como", "horas tentando", "sempre falha",
			"fazer eu mesmo", "sozinho", "frustrado", "nada funciona", "tentei sozinho",
		},
		SellerSignals: []string{
			"ofereço", "ofereco", "meus serviços", "meus servicos", "nossos serviços",
			"nossos servicos", "meu portfólio", "meu portfolio", "entre em contato", "me chama",
			"eu faço", "eu faco", "nós fazemos", "nos fazemos", "tabela de preços",
			"tabela de precos", "valores", "cases", "garantido", "melhor preço", "melhor preco",
			"barato", "acessível", "acessivel", "especializado em",
		},
		HRSignals: []string{
			"vaga", "estamos contratando", "procuramos desenvolvedor", "currículo", "curriculo",
			"cv", "salário", "salario", "tempo integral", "meio período", "meio periodo",
			"trabalho remoto", "envie seu cv", "recrutador", "estágio", "estagio", "efetivo",
		},
		SpamSignals: []string{
			"criptomoeda", "cripto", "p2p", "usdt", "investimento", "lucro garantido",
			"renda passiva", "ganhe dinheiro rápido", "ganhe dinheiro rapido", "forex",
			"sinais", "cassino", "apostas", "sorteio", "telegram stars", "seguidores",
			"indicação", "indicacao", "airdrop",
		},
		ServiceSignals: []string{
			"captcha", "verifique que você é humano", "verifique que voce e humano", "bem-vindo",
			"bem vindo", "regras do chat", "entrou no grupo", "saiu do grupo", "mensagem fixada",
			"inscreva-se", "inscreva se", "mensagem automática", "mensagem automatica",
			"advertido", "banido",
		},
	},
	{
		Code: "pl",
		Name: "Polish",
		DetectWords: []string{
			"szukam", "potrzebuję", "potrzebuje", "dla", "że", "ze", "ktoś", "ktos", "cześć",
			"czesc", "dziękuję", "dziekuje", "budżet", "budzet", "strona", "programista",
			"kto", "może", "moze", "mój", "moj", "z", "nie", "proszę", "prosze", "pomoc",
		},
		StopWords: []string{
			"i", "w", "na", "nie", "że", "ze", "z", "do", "się", "sie", "to", "jest", "o", "a",
			"ale", "lub", "dla", "od", "po", "jak", "tak", "jestem", "mój", "moj", "mi", "ty",
			"wy", "on", "ona", "oni", "bardzo", "proszę", "prosze", "dziękuję", "dziekuje",
			"cześć", "czesc",
		},
		IntentSignals: []string{
			"szukam", "potrzebuję", "potrzebuje", "poszukuję", "poszukuje", "kto może",
			"kto moze", "kto umie", "polecacie", "znacie kogoś", "znacie kogos", "potrzebna pomoc",
			"zatrudnię", "zatrudnie", "budżet", "budzet", "zapłacę", "zaplace", "gotów zapłacić",
			"gotow zaplacic", "freelancer", "agencja", "wycena", "szukam specjalisty",
			"gdzie znaleźć", "gdzie znalezc", "polećcie", "poleccie",
		},
		DIYSignals: []string{
			"już próbowałem", "juz probowalem", "nie udaje się", "nie udaje sie", "nie działa",
			"nie dziala", "nie mogę tego ogarnąć", "nie moge tego ogarnac", "nie wiem jak",
			"godziny prób", "godziny prob", "ciągle błędy", "ciagle bledy", "zrobić samemu",
			"zrobic samemu", "sam nie dam rady", "frustracja", "nic nie działa", "nic nie dziala",
		},
		SellerSignals: []string{
			"oferuję", "oferuje", "moje usługi", "moje uslugi", "nasze usługi", "nasze uslugi",
			"moje portfolio", "napisz do mnie", "zrobię to", "zrobie to", "możemy zrobić",
			"mozemy zrobic", "cennik", "stawki", "realizacje", "gwarantowane", "najlepsza cena",
			"tanio", "niedrogo", "specjalizuję się", "specjalizuje sie",
		},
		HRSignals: []string{
			"oferta pracy", "rekrutujemy", "szukamy programisty", "cv", "wynagrodzenie",
			"pełny etat", "pelny etat", "pół etatu", "pol etatu", "praca zdalna", "wyślij cv",
			"wyslij cv", "rekruter", "staż", "staz", "umowa o pracę", "umowa o prace",
		},
		SpamSignals: []string{
			"kryptowaluta", "krypto", "p2p", "usdt", "inwestycje", "gwarantowany zysk",
			"pasywny dochód", "pasywny dochod", "szybki zarobek", "forex", "sygnały", "sygnaly",
			"kasyno", "zakłady", "zaklady", "losowanie", "telegram stars", "obserwujący",
			"obserwujacy", "polecenia", "airdrop",
		},
		ServiceSignals: []string{
			"captcha", "potwierdź że jesteś człowiekiem", "potwierdz ze jestes czlowiekiem",
			"witamy", "zasady czatu", "dołączył do grupy", "dolaczyl do grupy", "opuścił grupę",
			"opuszczil grupe", "przypięta wiadomość", "przypieta wiadomosc", "subskrybuj",
			"wiadomość automatyczna", "wiadomosc automatyczna", "ostrzeżenie", "ostrzezenie",
			"zbanowany",
		},
	},
	{
		Code: "tr",
		Name: "Turkish",
		DetectWords: []string{
			"arıyorum", "ariyorum", "lazım", "lazim", "için", "icin", "ve", "bir", "kimse",
			"merhaba", "teşekkürler", "tesekkurler", "bütçe", "butce", "site", "geliştirici",
			"gelistirici", "kim", "yapabilir", "benim", "ile", "değil", "degil", "lütfen",
			"lutfen", "yardım", "yardim",
		},
		StopWords: []string{
			"ve", "bir", "bu", "o", "da", "de", "için", "icin", "ile", "çok", "cok", "ama", "veya",
			"ne", "nasıl", "nasil", "ben", "sen", "biz", "siz", "onlar", "benim", "değil", "degil",
			"var", "yok", "gibi", "daha", "en", "teşekkürler", "tesekkurler", "merhaba",
		},
		IntentSignals: []string{
			"arıyorum", "ariyorum", "ihtiyacım var", "ihtiyacim var", "lazım", "lazim",
			"kim yapabilir", "kim bilir", "önerir misiniz", "onerir misiniz",
			"tavsiye eder misiniz", "yardım lazım", "yardim lazim", "işe almak", "ise almak",
			"bütçe", "butce", "ödemeye hazırım", "odemeye hazirim", "freelancer", "teklif",
			"uzman arıyorum", "uzman ariyorum", "nerede bulabilirim", "öneri", "oneri",
		},
		DIYSignals: []string{
			"denedim olmadı", "denedim olmadi", "yapamıyorum", "yapamiyorum", "çalışmıyor",
			"calismiyor", "bıktım", "biktim", "nasıl yapacağımı bilmiyorum",
			"nasil yapacagimi bilmiyorum", "saatlerce denedim", "hep hata veriyor",
			"kendim yapmak", "tek başıma", "tek basima", "sinir oldum", "hiçbir şey çalışmıyor",
			"hicbir sey calismiyor",
		},
		SellerSignals: []string{
			"hizmet veriyorum", "hizmetlerim", "hizmetlerimiz", "portföyüm", "portfoyum",
			"bana yazın", "bana yazin", "ben yaparım", "ben yaparim", "biz yaparız", "biz yapariz",
			"fiyat listesi", "ücretler", "ucretler", "referanslar", "garantili", "en iyi fiyat",
			"ucuz", "uygun fiyat", "uzmanım", "uzmanim",
		},
		HRSignals: []string{
			"iş ilanı", "is ilani", "eleman arıyoruz", "eleman ariyoruz", "geliştirici arıyoruz",
			"gelistirici ariyoruz", "özgeçmiş", "ozgecmis", "cv", "maaş", "maas", "tam zamanlı",
			"tam zamanli", "yarı zamanlı", "yari zamanli", "uzaktan çalışma", "uzaktan calisma",
			"cv gönderin", "cv gonderin", "işe alım", "ise alim", "staj", "kadrolu",
		},
		SpamSignals: []string{
			"kripto", "kripto para", "p2p", "usdt", "yatırım", "yatirim", "garantili kazanç",
			"garantili kazanc", "pasif gelir", "hızlı para kazanma", "hizli para kazanma",
			"forex", "sinyaller", "kumar", "bahis", "çekiliş", "cekiliş", "telegram stars",
			"takipçi", "takipci", "referans", "airdrop",
		},
		ServiceSignals: []string{
			"captcha", "insan olduğunuzu doğrulayın", "insan oldugunuzu dogrulayin",
			"hoş geldiniz", "hos geldiniz", "sohbet kuralları", "sohbet kurallari",
			"gruba katıldı", "gruba katildi", "gruptan ayrıldı", "gruptan ayrildi",
			"sabitlenmiş mesaj", "sabitlenmis mesaj", "abone ol", "otomatik mesaj", "uyarı",
			"uyari", "yasaklandı", "yasaklandi",
		},
	},
	{
		Code: "it",
		Name: "Italian",
		DetectWords: []string{
			"cerco", "bisogno", "per", "che", "qualcuno", "ciao", "grazie", "budget", "sito",
			"sviluppatore", "chi", "può", "puo", "mio", "con", "non", "aiuto", "favore", "sto",
			"cercando", "ho",
		},
		StopWords: []string{
			"il", "lo", "la", "i", "gli", "le", "di", "del", "della", "che", "e", "in", "un",
			"una", "per", "con", "non", "si", "è", "e", "al", "come", "più", "piu", "ma", "suo",
			"mia", "io", "tu", "noi", "voi", "molto", "grazie", "ciao",
		},
		IntentSignals: []string{
			"cerco", "ho bisogno", "mi serve", "chi può", "chi puo", "qualcuno può",
			"qualcuno puo", "consigliate", "raccomandazioni", "qualcuno conosce", "aiuto per",
			"assumere", "budget", "disposto a pagare", "freelancer", "preventivo",
			"cerco uno specialista", "dove trovo", "conoscete qualcuno", "ho bisogno di aiuto",
			"mi consigliate",
		},
		DIYSignals: []string{
			"già provato", "gia provato", "non riesco", "non funziona", "sono stanco di",
			"non so come", "ore a provare", "sempre errori", "farlo da solo", "da solo",
			"frustrato", "niente funziona", "provato da solo", "ho provato di tutto",
		},
		SellerSignals: []string{
			"offro", "i miei servizi", "i nostri servizi", "il mio portfolio", "contattami",
			"scrivimi", "lo faccio io", "facciamo", "listino prezzi", "tariffe", "referenze",
			"garantito", "miglior prezzo", "economico", "conveniente", "specializzato in",
		},
		HRSignals: []string{
			"offerta di lavoro", "stiamo assumendo", "cerchiamo sviluppatore", "curriculum",
			"cv", "stipendio", "tempo pieno", "part-time", "lavoro remoto", "invia il cv",
			"reclutatore", "stage", "assunzione", "unisciti al team",
		},
		SpamSignals: []string{
			"criptovaluta", "cripto", "p2p", "usdt", "investimento", "guadagno garantito",
			"reddito passivo", "guadagna velocemente", "forex", "segnali", "casinò", "casino",
			"scommesse", "sorteggio", "telegram stars", "follower", "referral", "airdrop",
		},
		ServiceSignals: []string{
			"captcha", "verifica di essere umano", "benvenuto", "regole della chat",
			"si è unito al gruppo", "si e unito al gruppo", "ha lasciato il gruppo",
			"messaggio fissato", "iscriviti", "messaggio automatico", "avvisato", "bannato",
		},
	},
	{
		Code: "ar",
		Name: "Arabic",
		DetectWords: []string{
			"أبحث", "ابحث", "محتاج", "عن", "في", "من", "هل", "أحد", "احد", "مرحبا", "شكرا",
			"ميزانية", "موقع", "مطور", "يمكن", "مع", "لا", "مساعدة", "من فضلك", "أحتاج", "احتاج",
		},
		StopWords: []string{
			"في", "من", "على", "إلى", "الى", "عن", "أن", "ان", "إن", "ان", "لا", "ما", "هذا",
			"هذه", "هو", "هي", "هم", "أنا", "انا", "نحن", "أنت", "انت", "مع", "أو", "او", "لكن",
			"كل", "كان", "يكون", "هل", "كيف", "لماذا", "شكرا", "مرحبا",
		},
		IntentSignals: []string{
			"أبحث عن", "ابحث عن", "أحتاج", "احتاج", "محتاج", "من يمكنه", "هل يوجد",
			"أنصحوني", "انصحوني", "توصيات", "هل يعرف أحد", "هل يعرف احد", "أحتاج مساعدة",
			"احتاج مساعدة", "أريد توظيف", "اريد توظيف", "ميزانية", "مستعد للدفع", "مستقل",
			"عرض سعر", "أبحث عن خبير", "ابحث عن خبير", "أين أجد", "اين اجد", "هل تعرفون",
		},
		DIYSignals: []string{
			"جربت", "لم أستطع", "لم استطع", "لا يعمل", "تعبت من", "لا أعرف كيف", "لا اعرف كيف",
			"ساعات من المحاولة", "دائما أخطاء", "دائما اخطاء", "أفعله بنفسي", "افعله بنفسي",
			"وحدي", "محبط", "لا شيء يعمل", "حاولت بنفسي",
		},
		SellerSignals: []string{
			"أقدم", "اقدم", "خدماتي", "خدماتنا", "ملف أعمالي", "ملف اعمالي", "تواصل معي",
			"راسلني", "أنا أستطيع", "انا استطيع", "نحن نقدم", "قائمة الأسعار", "قائمة الاسعار",
			"أسعار", "اسعار", "أعمال سابقة", "اعمال سابقة", "مضمون", "أفضل سعر", "افضل سعر",
			"رخيص", "مناسب", "متخصص في",
		},
		HRSignals: []string{
			"وظيفة", "نبحث عن موظف", "مطلوب مبرمج", "سيرة ذاتية", "راتب", "دوام كامل",
			"دوام جزئي", "عمل عن بعد", "أرسل سيرتك", "ارسل سيرتك", "توظيف", "تدريب", "تعيين",
			"انضم إلى فريقنا", "انضم الى فريقنا",
		},
		SpamSignals: []string{
			"عملة رقمية", "عملة رقميه", "كريبتو", "p2p", "usdt", "استثمار", "ربح مضمون",
			"دخل سلبي", "اربح المال بسرعة", "فوركس", "إشارات", "اشارات", "كازينو", "مراهنات",
			"سحب", "نجوم تيليجرام", "متابعين", "إحالة", "احالة", "إيردروب", "ايردروب",
		},
		ServiceSignals: []string{
			"كابتشا", "تأكد أنك إنسان", "تاكد انك انسان", "مرحبا بك", "قواعد الدردشة",
			"انضم إلى المجموعة", "انضم الى المجموعة", "غادر المجموعة", "رسالة مثبتة",
			"اشترك", "رسالة تلقائية", "تحذير", "محظور",
		},
	},
	{
		Code:        "zh",
		Name:        "Chinese",
		DetectWords: []string{},
		StopWords: []string{
			"的", "了", "是", "在", "我", "你", "他", "她", "我们", "你们", "这", "那", "和", "与",
			"就", "不", "也", "都", "会", "要", "有", "没有", "吗", "呢", "吧", "啊", "怎么",
			"什么", "为什么", "请", "谢谢", "你好",
		},
		IntentSignals: []string{
			"寻找", "我需要", "需要", "谁能", "有人能", "推荐", "求推荐", "有没有人",
			"需要帮助", "预算", "愿意付费", "自由职业", "报价", "找专家", "求助", "请问谁",
			"想找", "求介绍", "谁能做", "谁会做", "哪里找",
		},
		DIYSignals: []string{
			"试过了", "弄不好", "不会弄", "搞不定", "研究了很久", "一直失败", "自己弄",
			"累了", "崩溃", "试过很多次", "弄不明白", "白折腾", "自己尝试", "没弄好",
		},
		SellerSignals: []string{
			"我提供", "我的服务", "我们的服务", "我的作品", "联系我", "私信我", "我可以做",
			"我们可以做", "价格表", "报价单", "案例", "保证", "最优惠", "便宜", "擅长",
			"专业承接",
		},
		HRSignals: []string{
			"招聘", "招人", "职位", "简历", "薪资", "全职", "兼职", "远程工作",
			"发送简历", "人事", "实习", "入职", "岗位", "薪资待遇",
		},
		SpamSignals: []string{
			"加密货币", "区块链", "投资", "稳定收益", "被动收入", "快速赚钱", "外汇",
			"信号", "赌场", "博彩", "抽奖", "telegram stars", "粉丝", "推荐奖励", "空投",
			"稳赚", "带单",
		},
		ServiceSignals: []string{
			"验证码", "请验证", "欢迎", "群规", "加入了群组", "离开了群组", "置顶消息",
			"订阅", "机器人消息", "自动消息", "警告", "封禁", "已加入群聊",
		},
	},
	{
		Code:        "ko",
		Name:        "Korean",
		DetectWords: []string{},
		StopWords: []string{
			"이", "가", "을", "를", "은", "는", "에", "에서", "와", "과", "도", "로", "으로",
			"그리고", "하지만", "또는", "나", "너", "우리", "그", "그녀", "그들", "있다", "없다",
			"하다", "입니다", "매우", "감사합니다", "안녕하세요",
		},
		IntentSignals: []string{
			"찾고 있습니다", "찾고 있어요", "필요합니다", "필요해요", "누가 할 수 있나요",
			"누구 있어요", "추천해 주세요", "추천 부탁드립니다", "도와주세요", "예산",
			"지불할 의향", "프리랜서", "견적", "전문가를 찾습니다", "구합니다", "어디서 찾나요",
			"소개 부탁", "가능한 분",
		},
		DIYSignals: []string{
			"해봤는데", "안 됩니다", "안 돼요", "직접 해봤어요", "몇 시간째", "계속 실패",
			"어떻게 해야 할지 모르겠어요", "지쳤어요", "스트레스", "혼자 해봤어요",
			"아무것도 안 돼요", "실패만 해요",
		},
		SellerSignals: []string{
			"제공합니다", "제 서비스", "저희 서비스", "포트폴리오", "연락 주세요", "dm 주세요",
			"제가 할 수 있습니다", "저희가 합니다", "가격표", "요금", "사례", "보장",
			"최저가", "저렴", "전문 분야",
		},
		HRSignals: []string{
			"채용", "구인", "개발자 모집", "이력서", "급여", "정규직", "아르바이트",
			"재택근무", "이력서를 보내세요", "인사", "인턴", "입사", "채용 공고",
		},
		SpamSignals: []string{
			"암호화폐", "코인", "p2p", "usdt", "투자", "보장 수익", "수동 소득",
			"빠르게 돈 벌기", "외환", "신호", "카지노", "베팅", "추첨", "telegram stars",
			"팔로워", "추천인", "에어드롭",
		},
		ServiceSignals: []string{
			"캡차", "사람인지 확인", "환영합니다", "채팅 규칙", "그룹에 참여했습니다",
			"그룹을 떠났습니다", "고정 메시지", "구독하세요", "봇 메시지", "자동 메시지",
			"경고", "차단됨",
		},
	},
	{
		Code:        "ja",
		Name:        "Japanese",
		DetectWords: []string{},
		StopWords: []string{
			"の", "に", "は", "を", "た", "が", "で", "て", "と", "し", "れ", "さ", "ある",
			"いる", "も", "する", "から", "な", "こと", "として", "い", "や", "れる", "など",
			"なっ", "ない", "この", "ため", "その", "あっ", "よう", "また", "もの", "という",
			"あり", "まで", "られ", "なる", "へ", "か", "だ", "これ", "です", "ます",
		},
		IntentSignals: []string{
			"探しています", "探してます", "必要", "誰かできますか", "おすすめ", "教えてください",
			"助けて", "予算", "支払います", "フリーランス", "見積もり", "専門家を探しています",
			"困っています", "どこで探せば", "紹介してください", "できる方",
		},
		DIYSignals: []string{
			"試しました", "できません", "うまくいかない", "自分でやってみた", "何時間も",
			"失敗ばかり", "わからない", "疲れた", "イライラ", "自分で試した", "全然できない",
			"うまくできない",
		},
		SellerSignals: []string{
			"提供します", "私のサービス", "私たちのサービス", "ポートフォリオ", "連絡してください",
			"dmください", "できます", "価格表", "料金", "実績", "保証", "最安値", "安い",
			"専門です",
		},
		HRSignals: []string{
			"求人", "採用", "募集", "履歴書", "給与", "正社員", "アルバイト", "リモートワーク",
			"応募", "人事", "インターン", "入社", "求人情報",
		},
		SpamSignals: []string{
			"仮想通貨", "暗号資産", "投資", "確実な利益", "不労所得", "すぐ稼げる", "為替",
			"シグナル", "カジノ", "賭け", "抽選", "telegram stars", "フォロワー", "紹介",
			"エアドロップ",
		},
		ServiceSignals: []string{
			"認証", "ロボットではありません", "ようこそ", "グループルール", "グループに参加しました",
			"グループを退出しました", "固定メッセージ", "登録", "ボットメッセージ", "自動メッセージ",
			"警告", "禁止",
		},
	},
}
