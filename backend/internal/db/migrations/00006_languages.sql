-- Languages (decision 029): the catalog of languages a member can choose,
-- and each member's own languages, spoken and learning, with a level. Like
-- the profile's text they are meant to become visible to other members.

-- +goose Up
-- The catalog. A language is identified by its code and never by a name: the
-- BCP 47 primary language subtag in lowercase, which is the ISO 639-1
-- two-letter code where one exists and the ISO 639-3 three-letter code
-- otherwise (yue, fil). Scripts and regional variants are not codes.
--
-- Rows are added by migrations only (a new numbered file with INSERTs), so
-- every environment has the same catalog. name and endonym are data shown
-- when choosing a language, not localized strings.
CREATE TABLE languages (
    code       text        PRIMARY KEY,
    name       text        NOT NULL, -- English reference name
    endonym    text        NOT NULL, -- the language's name in the language itself
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT languages_code_format  CHECK (code ~ '^[a-z]{2,3}$'),
    CONSTRAINT languages_name_key     UNIQUE (name),
    CONSTRAINT languages_name_text    CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 60),
    CONSTRAINT languages_endonym_text CHECK (endonym = btrim(endonym) AND char_length(endonym) BETWEEN 1 AND 60)
);

INSERT INTO languages (code, name, endonym) VALUES
    ('af',  'Afrikaans',          'Afrikaans'),
    ('sq',  'Albanian',           'Shqip'),
    ('am',  'Amharic',            'አማርኛ'),
    ('ar',  'Arabic',             'العربية'),
    ('hy',  'Armenian',           'Հայերեն'),
    ('as',  'Assamese',           'অসমীয়া'),
    ('az',  'Azerbaijani',        'Azərbaycanca'),
    ('eu',  'Basque',             'Euskara'),
    ('be',  'Belarusian',         'Беларуская'),
    ('bn',  'Bengali',            'বাংলা'),
    ('bs',  'Bosnian',            'Bosanski'),
    ('bg',  'Bulgarian',          'Български'),
    ('my',  'Burmese',            'မြန်မာဘာသာ'),
    ('yue', 'Cantonese',          '粵語'),
    ('ca',  'Catalan',            'Català'),
    ('ceb', 'Cebuano',            'Cebuano'),
    ('zh',  'Chinese (Mandarin)', '中文'),
    ('hr',  'Croatian',           'Hrvatski'),
    ('cs',  'Czech',              'Čeština'),
    ('da',  'Danish',             'Dansk'),
    ('nl',  'Dutch',              'Nederlands'),
    ('en',  'English',            'English'),
    ('et',  'Estonian',           'Eesti'),
    ('fil', 'Filipino (Tagalog)', 'Filipino'),
    ('fi',  'Finnish',            'Suomi'),
    ('fr',  'French',             'Français'),
    ('gl',  'Galician',           'Galego'),
    ('ka',  'Georgian',           'ქართული'),
    ('de',  'German',             'Deutsch'),
    ('el',  'Greek',              'Ελληνικά'),
    ('gn',  'Guarani',            'Avañeʼẽ'),
    ('gu',  'Gujarati',           'ગુજરાતી'),
    ('ht',  'Haitian Creole',     'Kreyòl ayisyen'),
    ('ha',  'Hausa',              'Hausa'),
    ('haw', 'Hawaiian',           'ʻŌlelo Hawaiʻi'),
    ('he',  'Hebrew',             'עברית'),
    ('hi',  'Hindi',              'हिन्दी'),
    ('hu',  'Hungarian',          'Magyar'),
    ('is',  'Icelandic',          'Íslenska'),
    ('ig',  'Igbo',               'Igbo'),
    ('id',  'Indonesian',         'Bahasa Indonesia'),
    ('ga',  'Irish',              'Gaeilge'),
    ('it',  'Italian',            'Italiano'),
    ('ja',  'Japanese',           '日本語'),
    ('jv',  'Javanese',           'Basa Jawa'),
    ('kn',  'Kannada',            'ಕನ್ನಡ'),
    ('kk',  'Kazakh',             'Қазақ тілі'),
    ('km',  'Khmer',              'ខ្មែរ'),
    ('rw',  'Kinyarwanda',        'Ikinyarwanda'),
    ('ko',  'Korean',             '한국어'),
    ('ku',  'Kurdish',            'Kurdî'),
    ('ky',  'Kyrgyz',             'Кыргызча'),
    ('lo',  'Lao',                'ລາວ'),
    ('lv',  'Latvian',            'Latviešu'),
    ('ln',  'Lingala',            'Lingála'),
    ('lt',  'Lithuanian',         'Lietuvių'),
    ('lb',  'Luxembourgish',      'Lëtzebuergesch'),
    ('mk',  'Macedonian',         'Македонски'),
    ('mg',  'Malagasy',           'Malagasy'),
    ('ms',  'Malay',              'Bahasa Melayu'),
    ('ml',  'Malayalam',          'മലയാളം'),
    ('mt',  'Maltese',            'Malti'),
    ('mi',  'Maori',              'Te reo Māori'),
    ('mr',  'Marathi',            'मराठी'),
    ('mn',  'Mongolian',          'Монгол'),
    ('ne',  'Nepali',             'नेपाली'),
    ('no',  'Norwegian',          'Norsk'),
    ('or',  'Odia',               'ଓଡ଼ିଆ'),
    ('om',  'Oromo',              'Afaan Oromoo'),
    ('ps',  'Pashto',             'پښتو'),
    ('fa',  'Persian',            'فارسی'),
    ('pl',  'Polish',             'Polski'),
    ('pt',  'Portuguese',         'Português'),
    ('pa',  'Punjabi',            'ਪੰਜਾਬੀ'),
    ('qu',  'Quechua',            'Runa Simi'),
    ('ro',  'Romanian',           'Română'),
    ('ru',  'Russian',            'Русский'),
    ('gd',  'Scottish Gaelic',    'Gàidhlig'),
    ('sr',  'Serbian',            'Српски'),
    ('sn',  'Shona',              'chiShona'),
    ('sd',  'Sindhi',             'سنڌي'),
    ('si',  'Sinhala',            'සිංහල'),
    ('sk',  'Slovak',             'Slovenčina'),
    ('sl',  'Slovenian',          'Slovenščina'),
    ('so',  'Somali',             'Soomaali'),
    ('es',  'Spanish',            'Español'),
    ('sw',  'Swahili',            'Kiswahili'),
    ('sv',  'Swedish',            'Svenska'),
    ('tg',  'Tajik',              'Тоҷикӣ'),
    ('ta',  'Tamil',              'தமிழ்'),
    ('tt',  'Tatar',              'Татарча'),
    ('te',  'Telugu',             'తెలుగు'),
    ('th',  'Thai',               'ไทย'),
    ('bo',  'Tibetan',            'བོད་སྐད་'),
    ('ti',  'Tigrinya',           'ትግርኛ'),
    ('tr',  'Turkish',            'Türkçe'),
    ('tk',  'Turkmen',            'Türkmençe'),
    ('uk',  'Ukrainian',          'Українська'),
    ('ur',  'Urdu',               'اردو'),
    ('ug',  'Uyghur',             'ئۇيغۇرچە'),
    ('uz',  'Uzbek',              'Oʻzbekcha'),
    ('vi',  'Vietnamese',         'Tiếng Việt'),
    ('cy',  'Welsh',              'Cymraeg'),
    ('wo',  'Wolof',              'Wolof'),
    ('xh',  'Xhosa',              'isiXhosa'),
    ('yo',  'Yoruba',             'Yorùbá'),
    ('zu',  'Zulu',               'isiZulu');

-- A member's languages: one row per user and language. No row at all means
-- the member hasn't chosen any yet.
CREATE TABLE user_languages (
    user_id       uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- No ON DELETE: a language somebody has can't be removed from the catalog.
    language_code text        NOT NULL REFERENCES languages (code),
    -- 'spoken': the member knows it and can offer it. 'learning': the member
    -- wants to practise it. A language is in only one of the two (the
    -- primary key), and a learner's level says how much they already know.
    kind          text        NOT NULL,
    -- CEFR, then native, as one ordered scale so levels compare as numbers:
    -- 1 = A1, 2 = A2, 3 = B1, 4 = B2, 5 = C1, 6 = C2, 7 = native.
    level         smallint    NOT NULL,
    -- The member's own order within a kind, 0 first. Its range is also what
    -- limits a member to five languages of each kind.
    position      smallint    NOT NULL,
    -- A member's set is replaced whole, so this is when the set was saved.
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT user_languages_pkey             PRIMARY KEY (user_id, language_code),
    CONSTRAINT user_languages_kind_check       CHECK (kind IN ('spoken', 'learning')),
    CONSTRAINT user_languages_level_range      CHECK (level BETWEEN 1 AND 7),
    -- Nobody is learning a language they are native in.
    CONSTRAINT user_languages_native_is_spoken CHECK (level < 7 OR kind = 'spoken'),
    CONSTRAINT user_languages_position_range   CHECK (position BETWEEN 0 AND 4),
    CONSTRAINT user_languages_position_key     UNIQUE (user_id, kind, position)
);

-- Who speaks or learns a language, and at which level: the lookup discovery
-- and matching will make. It also indexes the foreign key to the catalog.
-- The primary key serves the other direction, a member's own languages.
CREATE INDEX user_languages_by_language ON user_languages (language_code, kind, level);

-- +goose Down
-- Deletes every member's languages and the catalog. For development; a
-- production rollback redeploys the previous binary, which never touches
-- these tables.
DROP TABLE user_languages;
DROP TABLE languages;
