---
title: Log audit
category: security
categoryLabel: Keselamatan
summary: Apa yang direkodkan, apa maksud nama tindakan, dan berapa lama ia disimpan.
order: 330
---

# Log audit

Log audit ialah rekod tambah-sahaja tentang apa yang berlaku pada pelayan ini: setiap log masuk,
setiap penolakan, setiap perubahan kepada siapa boleh buat apa. Ia boleh dibaca dan ia boleh
dilupuskan mengikut usia, tetapi tiada apa-apa dalam produk ini mengedit atau memadam satu catatan
pun.

Ia **khusus superadmin** atas sebab yang sama seperti APInya. Jejak itu menamakan siapa buat apa dari
mana, dan pada pelayan identiti ia turut mendedahkan nama pengguna mana yang wujud.

## Membacanya {#reading}

**Sistem › Log audit**, terbaharu dahulu. Tapis mengikut tindakan, hasil (berjaya, ditolak, ralat),
siapa, dan julat tarikh; **Eksport CSV** menggunakan penapis yang sama, jadi apa yang anda eksport
ialah apa yang anda sedang lihat.

Setiap catatan membawa siapa, apa, sasaran yang mana, dari alamat yang mana, dengan pelayar yang
mana, dan bila. Catatan tanpa pelaku bukan kecacatan: sesetengah perkara memang berlaku tanpa sesiapa
log masuk — lihat [penanda pemulihan](first-sign-in#reset-admin).

## Nama tindakan {#actions}

Nama-nama itu ialah perbendaharaan kata tertutup dan bukan teks bebas, itulah yang menjadikannya
berbaloi ditapis. Ini yang berbaloi dikenali dengan pandangan:

```spec
title : Nama tindakan yang benar-benar akan anda tapis
row login-failure `login.failure` : Satu kelayakan ditolak. Rentetan ini terhadap satu akaun ialah percubaan tekaan; rentetan merentasi banyak akaun dari satu alamat ialah semburan.
row login-lockout `login.lockout` : Satu alamat mencapai had kegagalan dan dikunci.
row mfa-recovery `mfa.recovery_used` : Seseorang menggunakan kod pemulihan sekali guna. Direkodkan berasingan daripada log masuk biasa dengan sengaja — ia isyarat terkuat bahawa pengesah telah hilang atau diambil alih.
row mfa-admin-reset `mfa.admin_reset` : Faktor kedua sesuatu akaun dibersihkan oleh orang selain pemiliknya, atau oleh penanda RESET_MFA pada cakera.
row stepup-failure `stepup.failure` : Satu sesi gagal membuktikan semula kelayakannya. Inilah yang dihasilkan oleh seseorang yang menduga kuki yang dicuri.
row role-change `user.role_change` : Peranan seseorang ditukar — suntingan paling berkaitan keistimewaan pada pelayan ini.
row sso-refused `sso.refused` : Log masuk aplikasi bergantung ditolak: URI ubah hala tidak berdaftar, klien tidak dikenali, rahsia salah, atau kod dimain semula.
row backup-export `backup.export` : Keseluruhan stor identiti meninggalkan pelayan ini dalam satu fail.
row audit-purge `audit.retention_purge` : Kerja pengekalan memangkas jejak. Kehadirannya ialah yang membezakan log yang dipangkas daripada log yang sejarahnya memang bermula di situ.
```

Bersama nama itu, catatan pengesahan merekodkan **bagaimana** orang itu masuk — tempatan, `ldap`,
Kerberos, OIDC, sosial, atau kod pemulihan — supaya "bagaimana mereka log masuk?" boleh dijawab tanpa
merujuk silang konfigurasi seperti keadaannya pada hari itu.

## Siapa dibenarkan masuk ke aplikasi yang mana {#federation}

Inilah bahagian yang hanya pelayan identiti boleh jawab, dan ia mudah terlepas pandang.

"Akaun ini telah dikompromi — apa yang dicapainya?" bukan soalan yang boleh dijawab oleh aplikasi
bergantung; setiap satu hanya pernah melihat sesi muncul begitu sahaja. MyIDSan merekodkan
`sso.authorize` dan `sso.token_issue` terhadap **aplikasi** yang dibuka oleh setiap log masuk, jadi
jejak itu menyatakan bukan sekadar bahawa seseorang log masuk tetapi apa yang ditukar dengan log
masuk itu.

Penolakan (`sso.refused`) boleh dikatakan separuh yang lebih berguna. URI ubah hala tidak berdaftar,
klien tidak dikenali dan kod kebenaran yang dimain semula ialah rupa serangan terhadap aliran itu,
dan jejak yang hanya memegang kejayaan tidak boleh menunjukkannya. Apabila aplikasi bergantung
melaporkan kegagalan log masuk yang lognya sendiri tidak dapat jelaskan, di sinilah sebabnya. Lihat
[Menyambung aplikasi](connecting-an-app#troubleshooting).

## Berapa lama ia disimpan {#retention}

Secara lalai, **selama-lamanya**. Tiada apa-apa dipangkas melainkan pengekalan dihidupkan dalam
`config.json`, kerana pertumbuhan tanpa had menelan cakera manakala sejarah keselamatan yang hilang
menelan satu penyiasatan.

Apabila ia dihidupkan, baris yang melepasi had usia **diarkibkan ke fail dahulu** dan kemudian
dibuang daripada jadual, dan larian itu sendiri direkodkan. Had minimum ialah 30 hari — nilai yang
lebih pendek dinaikkan kepadanya, dengan amaran semasa permulaan, kerana jejak yang lebih pendek
daripada itu menjawab sangat sedikit.

> [!IMPORTANT]
> Fail arkib memegang alamat e-mel, alamat sumber dan ejen pengguna dalam bentuk jelas. Ia sengaja
> tidak dimeterai dengan kunci semasa-rehat hos ini — arkib yang kuncinya hanya wujud pada mesin yang
> sepatutnya ia hidup lebih lama daripadanya bukanlah arkib — jadi lindungi direktori itu dengan
> sewajarnya, dan masukkannya dalam apa jua yang anda sandarkan.

Arkib itu berasingan daripada [Sandaran dan pemulihan](backup-restore), yang langsung tidak membawa
jejak audit.

## Ke mana seterusnya {#next}

- [Sesi dan pengesahan bertingkat](sessions-and-step-up) — menamatkan sesi yang membuatkan jejak itu
  menimbulkan syak anda.
- [Sandaran dan pemulihan](backup-restore) — separuh lagi sesuatu insiden: mendapatkan pelayan
  kembali.
