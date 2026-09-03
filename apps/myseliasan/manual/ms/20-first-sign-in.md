---
title: Log masuk buat kali pertama
category: getting-started
categoryLabel: Permulaan
summary: Cari kata laluan permulaan sekali guna, kemudian serahkan kepada akaun sebenar.
order: 20
---

# Log masuk buat kali pertama

## Akaun permulaan {#bootstrap}

Kali pertama MySeliaSan dimulakan, ia mencipta satu akaun **superadmin** dengan kata laluan yang
**dijana untuk pemasangan ini**. Tiada kata laluan lalai yang dihantar untuk dicari, dan tiada dua
pemasangan berkongsi kata laluan yang sama.

Kata laluan diletakkan di dua tempat, supaya anda boleh menemuinya mengikut cara anda menjalankan
satah kawalan:

- **Pada konsol.** Sepanduk dicetak semasa permulaan dengan alamat untuk dibuka, nama pengguna dan
  kata laluan. Dalam Docker ia berada dalam `docker logs`; pada Linux, dalam jurnal perkhidmatan.
- **Dalam fail.** Fail kelayakan ditulis ke dalam direktori data — gunakan ini apabila konsol sudah
  berlalu atau perkhidmatan berjalan tanpa tetingkap yang kelihatan.

> [!NOTE]
> Jika anda menetapkan `localAuth` dalam `config.json`, atau pemboleh ubah persekitaran
> `LOCAL_ADMIN_PASSWORD`, sebelum permulaan pertama, kata laluan itu digunakan sebaliknya dan
> **tidak** dipaparkan di mana-mana. Sepanduk itu menunjuk kepada konfigurasi anda dan bukannya
> mencetak rahsia yang sudah anda miliki.

Akaun itu ditanda *mesti tukar kata laluan*, jadi perkara pertama yang anda lihat selepas log masuk
ialah skrin tukar kata laluan. Masukkan kata laluan sekali guna, kemudian kata laluan anda dua kali.

## Akaun ini bersifat sementara {#handover}

Superadmin permulaan wujud untuk membolehkan anda masuk. Ia bukan akaun yang sepatutnya anda gunakan
untuk mentadbir estet, dan bestari larian pertama mempunyai langkah **Penyerahan** khusus untuk itu.

Keadaan akhir yang dihasratkan ialah orang sebenar log masuk sebagai diri mereka sendiri dan akaun
permulaan disingkirkan. Sehingga itu berlaku, setiap tindakan dalam log audit dikaitkan dengan akaun
yang dikongsi, yang menjadikan log audit itu jauh kurang berguna daripada sepatutnya.

Lihat [Bestari persediaan kali pertama](setup-wizard#handoff).

## Log masuk dengan pelayan identiti anda {#sso}

MySeliaSan boleh menyerahkan pengesahan kepada **MyIDSan**, supaya orang log masuk dengan akaun yang
sudah mereka miliki.

Anda tidak menaip butiran sambungan dua kali. Halaman Apps MyIDSan mengeksport klien yang
didaftarkannya untuk satah kawalan ini sebagai fail JSON kecil; import fail itu di sini dan pengeluar,
audiens, ID klien dan URL ubah hala diisi tepat seperti yang ditulis MyIDSan. Dua konsol, satu sumber
kebenaran.

Jika anda tidak menjalankan pelayan identiti, langkau — akaun tempatan terus berfungsi, dan anda
boleh menyambungkannya kemudian.

> [!NOTE]
> Lompatan log masuk ke MyIDSan ialah satu-satunya tempat pelayar meninggalkan satah kawalan, dan ia
> kekal di dalam rangkaian anda sendiri. Tiada apa dalam laluan ini yang mencapai internet.

## Log masuk harian {#daily}

Skrin log masuk membawa anda sama ada ke borang akaun tempatan atau keluar ke pelayan identiti anda,
bergantung pada cara satah kawalan dikonfigurasikan. Dua kawalan berada di sekelilingnya, kedua-duanya
diingati pada pelayar ini:

- **Penukar bahasa** — Inggeris, Melayu, Cina dan Arab. Bahasa Arab mencerminkan susun atur.
- **Pautan bantuan**, yang membuka manual ini. Ia berfungsi sebelum anda log masuk, iaitu waktu anda
  paling mungkin memerlukannya.

## Apabila log masuk gagal {#troubleshooting}

- **"Akaun anda tiada peranan"** — akaun sebenar wujud tetapi tiada sesiapa memberikannya peranan
  lagi. Itu disengajakan: pengguna baharu bermula tanpa apa-apa dan bukan mewarisi capaian. Pentadbir
  menetapkannya di bawah Peranan & Capaian.
- **Kegagalan berulang mengunci alamat itu** untuk tempoh yang bertambah dengan setiap percubaan.
  Tunggu kiraan detik; tiada sesiapa boleh memendekkannya. **Akaun** dikira berasingan daripada
  alamat, jadi seseorang yang meneka nama pengguna anda dari tempat lain boleh menguncinya walaupun
  anda tidak tersilap taip. Itu kos yang disengajakan untuk menghentikan serangan tekaan yang
  tersebar merentas banyak alamat, dan ia terbuka semula sebaik sahaja sesiapa log masuk dengan
  betul. Menukar kata laluan anda sendiri dikira sama: kata laluan *semasa* yang salah berulang kali
  turut menguncinya.
- **Ubah hala daripada pelayan identiti gagal** — biasanya sijil yang tidak dipercayai satah kawalan,
  atau URL ubah hala yang tidak sepadan dengan tepat. Import semula berkas SSO dan bukan menaip
  semula medan.

## Ke mana selepas ini {#next}

- [Apa yang berlaku pada permulaan pertama](first-start) — halaman persediaan yang berjalan sebelum yang ini.
- [Bestari persediaan kali pertama](setup-wizard) — apa yang dilakukan enam langkah itu.
- [Lawatan ruang kerja](workspace-tour) — apa yang anda lihat setelah berada di dalam.
