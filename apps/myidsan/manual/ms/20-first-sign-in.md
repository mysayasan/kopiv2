---
title: Log masuk buat kali pertama
category: getting-started
categoryLabel: Bermula
summary: Cari kata laluan permulaan sekali guna, dan fahami skrin yang boleh menahan anda.
order: 20
---

# Log masuk buat kali pertama

## Akaun permulaan {#bootstrap}

Kali pertama MyIDSan dimulakan, ia mencipta satu akaun **superadmin** dengan kata laluan yang
**dijana untuk pemasangan ini**. Tiada kata laluan lalai yang dihantar bersama untuk dicari, dan
tiada dua pemasangan berkongsi satu.

Kata laluan itu diletakkan di dua tempat, supaya anda boleh menemuinya dengan apa cara pun anda
menjalankan pelayan:

- **Pada konsol.** Sepanduk dicetak semasa permulaan dengan alamat untuk dibuka, nama pengguna dan
  kata laluan. Dalam Docker itu ialah `docker logs`; pada Linux, jurnal perkhidmatan.
- **Dalam fail.** `INITIAL_ADMIN_LOGIN.txt` ditulis ke dalam direktori data, boleh dibaca hanya oleh
  akaun yang menjalankan pelayan. Gunakan ini apabila konsol sudah menatal hilang atau perkhidmatan
  berjalan sebagai perkhidmatan Windows tanpa tetingkap yang kelihatan. **Padamkannya sebaik anda
  log masuk.**

> [!NOTE]
> Jika anda menetapkan `localAuth` dalam `config.json`, atau pemboleh ubah persekitaran
> `LOCAL_ADMIN_PASSWORD`, sebelum permulaan pertama, kata laluan itu digunakan sebaliknya dan **tidak**
> digemakan di mana-mana. Sepanduk itu menunjuk kepada konfigurasi anda dan bukannya mencetak rahsia
> yang anda sudah pegang.

Akaun itu ditandakan *mesti tukar kata laluan*, jadi perkara pertama yang anda lihat selepas log
masuk ialah skrin tukar kata laluan. Masukkan kata laluan sekali guna itu, kemudian kata laluan anda
sendiri dua kali.

## Log masuk tidak semestinya bermakna sudah sampai {#gates}

Kata laluan yang betul memberi anda sesi. Ia tidak semestinya memberi anda ruang kerja: ada empat
perkara berasingan yang boleh menahan anda pada skrin tersendiri, dan ia diperiksa mengikut susunan
yang tetap. Mengetahui susunan itu biasanya jawapan penuh kepada "saya sudah log masuk tetapi
tersekat".

```flow
title : Mengapa kata laluan yang betul masih boleh meninggalkan anda pada skrin tersendiri
step creds : Anda memasukkan nama pengguna dan kata laluan
ask locked : Terlalu banyak kegagalan baru-baru ini dari alamat ini?
end lockout : Ditolak sehingga kiraan detik tamat
ask factor => second-factor : Adakah akaun ini sudah mempunyai faktor kedua?
step code => second-factor : Masukkan kod daripada pengesah anda, atau kod pemulihan
ask pwd : Adakah kata laluan ditandakan mesti-tukar?
step change : Tetapkan kata laluan anda sendiri sebelum apa-apa lagi
ask enrol => second-factor#policy : Adakah dasar menuntut faktor yang akaun ini tidak ada?
step enrolnow => second-factor#enrol : Daftarkan pengesah sebelum apa-apa lagi
ask role => users-roles-groups#pending : Adakah sesiapa sudah memberi akaun ini peranan?
end pending => users-roles-groups#pending : Menunggu pelepasan — pentadbir perlu menetapkan satu
ok workspace : Ruang kerja, menunjukkan hanya apa yang peranan anda boleh buka
creds -> locked
locked -> lockout : ya
locked -> factor : tidak
factor -> code : ya
factor -> pwd : tidak
code -> pwd
pwd -> change : ya
pwd -> enrol : tidak
change -> enrol
enrol -> enrolnow : ya
enrol -> role : tidak
enrolnow -> role
role -> pending : tidak
role -> workspace : ya
```

Dua yang mengejutkan orang ialah dua yang terakhir. **Pendaftaran** diminta *selepas* kata laluan
berjaya dan bukannya sebagai gantinya — dasar itu membuatkan anda MENAMBAH faktor, bukan
membuktikan satu yang anda tidak ada, jadi menghidupkan dasar itu tidak boleh mengunci keluar
pentadbir yang belum mempunyainya. **Pelepasan** bukan kesalahan: akaun baharu bermula tanpa
sebarang peranan dan bukannya mewarisi satu, jadi seseorang perlu memberikannya.

## Kuncian, dan siapa yang sebenarnya dikunci {#lockout}

Kegagalan berulang mengunci **alamat sumber** untuk tempoh yang bertambah setiap kali berulang.
Setiap kegagalan juga menanggung sedikit kelewatan tetap, sama ada akaun itu wujud atau tidak — itu
disengajakan, supaya masa tindak balas tidak boleh digunakan untuk mengetahui nama pengguna mana
yang benar.

```spec
title : Dasar log masuk, sebagaimana ia dihantar
row minlength `12 characters` : Kata laluan terpendek yang boleh dipilih seseorang. Peraturan kelas aksara (huruf besar, huruf kecil, digit, simbol) semuanya MATI secara lalai — frasa laluan yang lebih panjang mengalahkan kata laluan pendek dengan simbol dicantum. Kata laluan yang sama dengan nama pengguna sentiasa ditolak, apa pun konfigurasi lain.
row attempts `8` : Kegagalan dari satu alamat sebelum ia dikunci.
row window `300s` : Tetingkap masa kegagalan itu dikira.
row lockout `60s` : Kuncian pertama. Ia berganda setiap kali berulang.
row lockoutmax `3600s` : Tempoh terpanjang kuncian boleh berkembang.
row delay `400ms` : Ditambah pada setiap percubaan yang gagal, supaya nama pengguna yang salah dan kata laluan yang salah mengambil masa yang sama untuk ditolak.
```

Kesemua enam boleh diedit di bawah **Tetapan › Log masuk**. Dua perkara di sana berbaloi diketahui
sebelum anda mengubahnya: kuncian tidak boleh ditetapkan di bawah dua percubaan (satu akan mengunci
akaun pada salah taip pertamanya), dan had minimum kata laluan tidak boleh ditetapkan di bawah lapan.

> [!WARNING]
> Tiada sesiapa boleh memendekkan kuncian dari dalam aplikasi — bukan superadmin, bukan pengguna
> yang terkunci. Tunggu kiraan detik. Jika anda telah mengunci diri anda daripada akaun superadmin
> terakhir, gunakan pemulihan di bawah.

## Jika anda terkunci daripada satu-satunya superadmin {#reset-admin}

Kedua-dua jalan keluar ialah fail yang anda letakkan dalam direktori data. Ia digunakan pada
permulaan berikutnya dan dipadamkan sebelum ia bertindak, jadi ranap sistem tidak akan sesekali
menggunakannya semula di belakang anda.

- `RESET_ADMIN` — menjana semula kata laluan superadmin permulaan dan mengumumkan yang baharu dengan
  cara yang sama seperti permulaan pertama, sepanduk dan `INITIAL_ADMIN_LOGIN.txt` kedua-duanya.
- `RESET_MFA` — membersihkan faktor kedua superadmin permulaan sahaja, tanpa menyentuh kata laluan.
  Lihat [Faktor kedua dan pemulihan akaun](second-factor#lost).

Kedua-duanya memerlukan akses tulis kepada direktori data pada hos. Itulah kebenarannya: sesiapa
yang memilikinya sudah pun boleh membaca pangkalan data. Log audit merekodkan set semula itu tanpa
pelaku, dan itulah catatan yang jujur — tiada sesiapa log masuk untuk menyebabkannya.

## Log masuk hari ke hari {#daily}

Skrin log masuk menawarkan apa jua kaedah yang dikonfigurasikan: nama pengguna dan kata laluan
tempatan, direktori anda (LDAP/AD) jika ada yang disambungkan, butang Kerberos pada desktop yang
menyertai domain, dan mana-mana penyedia sosial yang telah dihidupkan. Di sekeliling kad itu
terdapat tiga kawalan, masing-masing diingati dalam pelayar ini dan bukan pada akaun:

- Penukar **bahasa** — Inggeris, Melayu, Cina dan Arab. Bahasa Arab mencerminkan susun atur.
- Pemilih **tema**, termasuk palet kontras tinggi.
- **Pautan bantuan**, yang membuka manual ini. Ia berfungsi sebelum anda log masuk, iaitu tepat
  ketika anda paling mungkin memerlukannya.

## Ke mana seterusnya {#next}

- [Pengguna, peranan dan kumpulan](users-roles-groups) — melepaskan akaun yang tertunggak itu.
- [Faktor kedua dan pemulihan akaun](second-factor) — pendaftaran, kod pemulihan, kunci keselamatan.
- [Menyambung aplikasi](connecting-an-app) — menghalakan aplikasi pertama anda ke pelayan ini.
