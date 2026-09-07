---
title: Apa itu MyIDSan
category: getting-started
categoryLabel: Bermula
summary: Satu-satunya pelayan yang menentukan siapa itu siapa, dan apa yang ditanya oleh aplikasi lain kepadanya.
order: 10
---

# Apa itu MyIDSan

MyIDSan ialah **pelayan identiti** untuk suite ini. Ia menyimpan akaun, memeriksa kata laluan dan
faktor kedua, dan memberitahu setiap aplikasi lain siapa yang baru tiba. Tiada apa-apa lagi dalam
suite ini yang menyimpan kata laluan.

Itu menjadikannya aplikasi terkecil dalam suite dan yang paling luas kesannya apabila gagal. Apabila
MyIDSan tidak beroperasi, tiada sesiapa boleh log masuk ke mana-mana aplikasi yang telah dihalakan
kepadanya — jadi kebanyakan manual ini adalah tentang dua perkara yang menghalang hal itu daripada
berlaku: melakukan log masuk pertama dengan betul, dan mampu membina semula pelayan daripada
sandaran.

## Apa yang ia lakukan {#does}

- **Akaun.** Akaun tempatan dengan kata laluan, dan akaun yang datang daripada pelayan LDAP atau
  Active Directory yang anda sudah jalankan. Lihat [Pengguna, peranan dan kumpulan](users-roles-groups)
  dan [Menyambung direktori](directory).
- **Faktor kedua.** Kod daripada aplikasi pengesah, atau kunci keselamatan. Lihat
  [Faktor kedua dan pemulihan akaun](second-factor).
- **Log masuk tunggal untuk aplikasi anda.** Aplikasi lain menghantar seseorang ke sini untuk log
  masuk dan menerima kembali token bertempoh pendek yang menyatakan siapa mereka. Lihat
  [Menyambung aplikasi](connecting-an-app).
- **Rekod semua itu.** Lihat [Log audit](audit-log).

## Apa yang ia tidak lakukan {#does-not}

Menyatakan hal ini dengan jelas menjimatkan satu petang kerja integrasi.

- **Ia bukan penyedia OpenID Connect umum.** Lompatan log masuk yang ditawarkannya ialah pertukaran
  kod kebenaran berbentuknya sendiri, diterangkan dalam
  [Menyambung aplikasi](connecting-an-app#endpoints). Pustaka klien OIDC siap sedia menjangkakan
  dokumen penemuan dan titik akhir JWKS; MyIDSan tidak menerbitkan satu pun. Aplikasi dalam suite
  ini dihantar bersama klien yang memahaminya.
- **Ia tidak menentukan apa yang anda boleh buat di dalam aplikasi lain.** Ia menyatakan siapa anda
  dan peranan yang anda pegang; setiap aplikasi memetakan peranan itu kepada kebenarannya sendiri.
  Peranan yang dinamakan sama dalam dua aplikasi boleh bermaksud dua perkara berbeza.
- **Ia tidak menghantar mel secara lalai.** Pemulihan akaun berfungsi sebagai barisan giliran
  operator tanpa pelayan mel langsung — lihat
  [Faktor kedua dan pemulihan akaun](second-factor#recovery).

## Di mana segala-galanya {#layout}

Bar di sebelah kiri dikumpulkan mengikut pembahagian kerja:

- **Pentadbiran** — Pengguna, Permintaan set semula, Kumpulan, Peranan, RBAC.
- **Persekutuan** — Aplikasi (yang log masuk melalui pelayan ini) dan Direktori.
- **Kawalan Akses** — Titik akhir, katalog yang menjadi asas pemberian peranan.
- **Sistem** — Log audit, Sandaran & pemulihan, Tetapan, dan manual ini.

Akaun anda sendiri — kata laluan, faktor kedua, kunci keselamatan, sesi anda — tiada dalam bar itu.
Ia di belakang cip di bahagian bawah bar, di bawah nama peranan anda.

> [!NOTE]
> Menu yang pendek bukan pepijat. Menu dibina daripada apa yang peranan anda benar-benar dibenarkan
> buka, jadi dua orang yang log masuk ke pelayan yang sama melihat menu berbeza. Jika sesuatu yang
> anda jangkakan tiada, itulah jawapannya: lihat
> [Pengguna, peranan dan kumpulan](users-roles-groups#matrix).

## Ke mana seterusnya {#next}

- [Log masuk buat kali pertama](first-sign-in) — kata laluan permulaan, dan skrin yang boleh
  menahan anda sebelum sampai ke ruang kerja.
- [Menyambung aplikasi](connecting-an-app) — mendaftarkan aplikasi bergantung yang pertama.
- [Sandaran dan pemulihan](backup-restore) — lakukan ini sebelum anda memerlukannya.
