---
title: Melatih model tersuai
category: detection
categoryLabel: Pengesanan & AI
summary: Bina set data, labelkannya, latih atau eksportkannya, dan aktifkan hasilnya.
order: 360
---

# Melatih model tersuai

Latihan ialah laluan manual ke destinasi yang sama dengan [Mod mengajar](teach-mode): model yang
mengenali sesuatu yang tidak diketahui model stok. Mod mengajar membungkus proses ini untuk orang
yang tidak mahu memikirkan set data. Halaman ini untuk apabila anda mahu.

## Runtime {#runtime}

Kedua-dua pengesanan dan latihan memerlukan runtime AI — Python berserta pustaka pengesanan —
dipasang daripada **Tetapan → AI**. Tanpanya, tiada apa yang dikesan dan tiada apa yang dilatih.

Latihan jauh lebih berat daripada pengesanan. Pada CPU ia diukur dalam jam; pada GPU yang baik,
beberapa minit hingga puluhan minit. Jika anda merancang untuk melatih secara berkala dan tiada GPU,
rancanglah untuk mengeksport set data dan melatihnya di tempat lain.

## Set data {#datasets}

Set data ialah satu set imej berserta kotak dan label yang dilukis padanya. Bina satu dengan:

- **Memuat naik imej** secara terus, atau
- **Mengimport gambar petikan amaran**, yang tiba sudah membawa kotak dan label pengesanan itu.

Yang kedua ialah yang kurang dihargai. Sejarah amaran anda ialah set data percuma yang sempurna pada
taburan: kamera sebenar, sudut sebenar, pencahayaan sebenar, cuaca sebenar. Imej daripada kamera itu
mengatasi gambar yang lebih baik daripada mana-mana tempat lain, kerana ia sepadan dengan apa yang
akan diberikan kepada model.

## Melabel {#labelling}

Lukis kotak di sekeliling setiap kejadian dan berikan label. Auto-label menjalankan model aktif ke
atas sesuatu imej dan mengisi terlebih dahulu apa yang ditemuinya, yang kemudian anda betulkan — jauh
lebih pantas daripada bermula dengan bingkai kosong.

Apa yang sebenarnya menentukan sama ada hasilnya berfungsi:

- **Konsisten.** Jika anda mengotakkan keseluruhan kenderaan dalam sesetengah imej dan hanya kabin
  dalam yang lain, anda telah mengajarnya dua perkara berbeza dan ia tidak akan melakukan
  kedua-duanya dengan baik.
- **Labelkan setiap kejadian dalam sesuatu imej.** Objek tidak berlabel dalam imej latihan sedang
  secara aktif mengajar model bahawa benda ini *bukan* kelas itu — lebih teruk daripada meninggalkan
  imej itu.
- **Sertakan negatif.** Imej tanpa objek anda, dan imej dengan benda yang kelihatan seperti ia tetapi
  bukan. Di sinilah kebanyakan pengurangan positif palsu datang.
- **Pelbagaikan segalanya.** Sudut, jarak, cahaya, cuaca, waktu. Model yang dilatih hanya pada imej
  tengah hari gagal pada waktu senja.

Skala anggaran: beberapa ratus kejadian yang pelbagai memberi sesuatu yang boleh digunakan; beberapa
dozen yang hampir serupa tidak.

## Label {#labels}

Gunakan label yang tersendiri dan bukan perkataan umum. Model yang dilatih pada `van` bersaing dengan
`truck` model stok ialah susunan yang mengelirukan; `acme-van` tidak.

Untuk menjadikan label itu dikira sebagai kategori sedia ada di mana-mana peraturan anda sudah
menggunakannya, tambahkannya kepada kategori itu dan bukan membiarkannya sebagai kelas peringkat atas
— lihat [Kelas objek](object-classes#filing).

## Melatih atau mengeksport {#training}

Latih di tempat, atau **eksport set data YOLO** — fail zip dengan `data.yaml` dan pembahagian
imej/label latihan/pengesahan — dan latih di tempat lain pada perkakasan yang lebih baik. Susun atur
yang dieksport ialah susun atur standard, jadi mana-mana alatan YOLO boleh membacanya.

Mengeksport ialah lalai yang betul bagi apa-apa yang melebihi set data kecil. Latih pada mesin
berGPU, bawa balik pemberatnya.

## Mengaktifkan model {#activating}

Import pemberat `.pt` dalam **Tetapan → AI** dan aktifkannya. Labelnya kemudian muncul dalam daftar
kelas objek dan boleh digunakan dalam peraturan.

Dua perkara berlaku serta-merta:

- **Setiap model aktif membuat inferens pada setiap bingkai.** Mengaktifkan model kedua kira-kira
  menggandakan kos pengesanan. Nyahaktifkan model yang tidak anda gunakan.
- **Pemberat model disimpan sebagai fail biasa**, tidak disulitkan seperti rakaman, kerana pekerja
  pengesanan membacanya secara terus. Model yang sensitif dari segi komersial patut dilayan
  sedemikian.

## Menilai sama ada ia berjaya {#evaluating}

Jangan nilai model berdasarkan imej yang dilatihnya — ia telah melihatnya.

Halakannya ke kamera sebenar, biarkan sehari, dan baca log amaran. Itulah satu-satunya ujian yang
meramalkan cara ia akan berkelakuan, dan ia biasanya mendedahkan dua perkara yang sama: sumber
positif palsu yang tiada sesiapa jangkakan, dan keadaan pencahayaan yang tiada dalam set data.
Kedua-duanya diperbaiki dengan menambah imej tersebut dan melatih semula, dan itulah sebabnya
membina set data daripada gambar petikan amaran anda sendiri berganda nilainya.
