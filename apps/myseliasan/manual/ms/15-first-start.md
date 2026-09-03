---
title: Apa yang berlaku pada permulaan pertama
category: getting-started
categoryLabel: Permulaan
summary: Pemasangan baharu mengkonfigurasi dirinya dalam pelayar, sebelum mustawa kawalan bermula.
order: 15
---

# Apa yang berlaku pada permulaan pertama

## Belum ada apa-apa dikonfigurasi {#wizard}

Mustawa kawalan yang baru dipasang tidak mempunyai pangkalan data, tiada cache, dan tiada
keputusan tentang port mana untuk disajikan. Semuanya dibaca sekali sahaja, semasa permulaan,
sebelum MySeliaSan memegang walau satu pemegang pangkalan data — itulah sebabnya semuanya tidak
boleh ditetapkan dari dalam aplikasi. Pada ketika itu, aplikasi belum ada.

Jadi permulaan pertama tidak memulakan mustawa kawalan. Ia membuka **halaman persediaan**
sebaliknya, pada port miliknya sendiri, dan menunggu anda. Apabila anda selesai, mustawa kawalan
bermula dalam proses yang sama, pada port yang baru anda pilih. Tiada apa-apa dimulakan semula.

## Mencari halaman persediaan {#finding-it}

Alamat diletakkan di tiga tempat, supaya anda boleh menemuinya mengikut cara anda menjalankannya:

- **Pelayar anda.** Ia terbuka sendiri pada pemasangan desktop.
- **Pada konsol.** Satu lafaz dicetak dengan alamatnya. Dalam Docker ia di `docker logs`; pada
  Linux, dalam jurnal perkhidmatan.
- **Dalam fail.** `SETUP_URL.txt` ditulis ke dalam direktori data dan dibuang setelah persediaan
  selesai. Gunakan ini apabila konsol sudah berlalu, atau perkhidmatan berjalan tanpa tetingkap
  yang kelihatan.

Halaman ini mendengar pada `127.0.0.1:39530` dan boleh dicapai **hanya dari mesin itu sendiri**.
Itu memang disengajakan: ia menerima kelayakan pangkalan data dan kata laluan pentadbir pertama,
dan belum ada akaun untuk log masuk. Jika anda perlu mencapainya dari mesin lain, tetapkan
`setup.allowRemote` — alamatnya kemudian membawa token sekali guna, dan anda patut menganggap
pautan itu sebagai kata laluan.

> [!NOTE]
> Jika sesuatu yang lain pada mesin itu sudah memegang port tersebut, persediaan berpindah ke port
> yang lapang dan menyatakannya pada konsol. Baca lafaz itu, jangan andaikan 39530.

## Apa yang ditanya {#answers}

Lima langkah, setiap satu sudah diisi dengan apa yang dibawa oleh pemasangan:

- **Pangkalan data** — PostgreSQL, MariaDB/MySQL, atau SQLite (satu fail, tanpa pelayan untuk
  dijalankan).
- **Cache** — dalam proses, atau Redis. Redis diperlukan hanya untuk menjalankan lebih daripada
  satu instans di belakang pengimbang beban.
- **Alamat web** — port HTTPS dan HTTP, dan nama hos yang hendak dilayan.
- **Pentadbir** — nama dan kata laluan akaun terbina dalam. Biarkan kata laluan kosong dan satu
  akan dijanakan untuk pemasangan ini.
- **Semak** — segala yang anda pilih, dalam satu senarai, sebelum apa-apa ditulis.

Langkah pangkalan data dan cache masing-masing mempunyai butang **Uji sambungan** yang membuat
sambungan sebenar. Gunakannya. Kata laluan yang salah dan ditemui di sini hanya memakan satu
klik; ditemui kemudian, ia menghalang mustawa kawalan daripada bermula.

Tiada apa-apa ditulis sehingga anda memilih **Selesai**. Meninggalkan halaman itu membiarkan
pemasangan tepat seperti keadaan asalnya.

## Menyelesaikannya {#then}

Selesai menulis jawapan anda ke dalam `config.json` dan memulakan mustawa kawalan. Pelayar anda
mengikutinya ke alamat baharunya dengan sendiri.

Hanya tetapan yang ditanya kepada anda ditulis — setiap bahagian lain fail itu mengekalkan nilai
sedia adanya. Kemudian teruskan dengan [Log masuk untuk kali pertama](first-sign-in#bootstrap).

> [!NOTE]
> Pemboleh ubah persekitaran tetap menang. Jika `DB_HOST`, atau mana-mana pemboleh ubah
> konfigurasi yang lain, ditetapkan dalam persekitaran, ia mengatasi apa yang anda taip di sini.
> Itulah yang anda mahukan dalam bekas, dan satu kejutan di tempat lain.

## Menjalankan persediaan sekali lagi {#again}

Persediaan berjalan sekali. Selepas itu pemasangan merekodkan bahawa ia sudah dikonfigurasi dan
terus bermula, dan tetapan diubah dari **Tetapan** di dalam aplikasi — lihat [Tetapan](settings).

Ada satu pengecualian, dan itulah sebabnya halaman ini boleh dibawa balik: jika mustawa kawalan
tidak boleh bermula sama sekali — pangkalan data yang berpindah, kata laluan Redis yang berubah —
mulakannya dengan `KOPIV2_SETUP=1` dan halaman persediaan kembali, sudah diisi, supaya anda boleh
memperbaiki tetapan yang menghalangnya daripada but.

Pangkalan data atau cache yang tidak dapat dicapai seketika tidak akan membawanya balik.
Persediaan muncul hanya apabila anda memintanya, atau pada pemasangan yang benar-benar belum
pernah dikonfigurasi.
