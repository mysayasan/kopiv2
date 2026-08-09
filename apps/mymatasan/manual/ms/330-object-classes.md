---
title: Kelas dan kumpulan objek
category: detection
categoryLabel: Pengesanan & AI
summary: Petakan label mentah model kepada nama yang anda guna, dan gabungkannya menjadi kumpulan.
order: 330
---

# Kelas dan kumpulan objek

Daftar di bawah **Objek → Kelas** ialah apa yang menukar perbendaharaan kata model kepada
perbendaharaan kata anda. Ia senarai yang ditawarkan oleh pemilih **Kesan** sesuatu peraturan.

## Kategori {#categories}

**Kategori** memetakan nama mesra kepada satu atau lebih label model mentah.

*Kenderaan* mungkin mengandungi `car`, `truck`, `bus` dan `motorcycle`. Peraturan yang mengesan
*Kenderaan* kemudiannya sepadan dengan mana-mana daripadanya, dan anda tidak perlu mengingati
perkataan mana yang digunakan model.

Dua jenis wujud: **kategori objek** (orang, kenderaan, …) dan **kategori bahaya** (api, asap), yang
merupakan mekanisme yang sama dilabelkan mengikut apa yang diwakilinya.

## Label {#labels}

Label ialah perkataan output tepat model, dan ketepatan itulah keseluruhan permainannya:

- Ia dipadankan dengan tepat. `fire hydrant`, bukan `fire_hydrant`.
- Label hanya mengesan sesuatu jika model yang menghasilkannya **aktif**. Daftar menanda kategori
  sebagai **model tidak aktif** apabila tiada apa yang berjalan mengeluarkan labelnya.
- Label yang tersalah taip tidak pernah sepadan dan tidak pernah mengadu. Ia hanya duduk di situ
  tanpa mengesan apa-apa.

Cari dan pilih daripada senarai, jangan menaip. Senarai itu ialah set label yang benar-benar
dihasilkan model aktif anda, jadi memilih daripadanya ialah satu-satunya cara untuk pasti.

Label daripada model yang anda latih muncul di sini sebaik model itu diaktifkan dalam
**Tetapan → AI**.

## Kumpulan {#groups}

**Kumpulan** menggabungkan beberapa kategori. Peraturan yang menyasarkan kumpulan itu sepadan dengan
mana-mana daripadanya.

*Trafik* = *Kenderaan* + *Orang* + *Basikal*, contohnya. Kumpulan berguna apabila beberapa kategori
sentiasa hadir bersama dalam peraturan anda — takrifkan gabungan itu sekali dan peraturan kekal
mudah dibaca.

## Memfailkan label terlatih di bawah kategori sedia ada {#filing}

Ini petua yang wajar diketahui.

Apabila anda melatih model dengan label anda sendiri — katakan `papa` untuk sebuah kenderaan tertentu
— ia muncul sebagai kelas peringkat atas sendiri secara lalai. Biasanya itu bukan yang anda mahukan:
anda mahu ia dikira sebagai *Kenderaan* di mana-mana *Kenderaan* sudah digunakan.

Edit kategori sedia ada dan tambah label itu di situ. Ia berhenti menjadi kelas peringkat atas yang
berasingan, dan setiap peraturan yang sudah mengesan *Kenderaan* akan mengambilnya tanpa sebarang
perubahan lanjut.

## Bentuk praktikal {#practice}

Kekalkan daftar itu kecil dan bermakna. Satu kategori bagi setiap perkara yang anda tulis peraturan
mengenainya, bukan satu kategori bagi setiap label yang boleh dihasilkan model.

Tapak yang berakhir dengan tiga puluh kategori biasanya mempunyai tiga yang digunakan dan dua puluh
tujuh yang menjadikan pemilih peraturan tidak boleh dibaca. Daftar ialah perbendaharaan kata untuk
peraturan anda — jika anda tidak akan menyebutnya melalui radio, ia mungkin tidak perlu menjadi
kategori.
