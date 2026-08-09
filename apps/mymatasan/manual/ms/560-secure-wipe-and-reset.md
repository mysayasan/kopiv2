---
title: Padam selamat dan set semula kilang
category: administration
categoryLabel: Pentadbiran
summary: Musnahkan segalanya pada peranti ini, secara sengaja — apa yang dilakukannya dan apa yang bertahan.
order: 560
---

# Padam selamat dan set semula kilang

Perkara paling memusnahkan dalam produk ini, dalam **Tetapan → Sandaran & Pemulihan**. Baca ini
sebelum anda menggunakannya, bukan semasa.

## Apa yang dilakukannya {#what}

Mengikut urutan:

1. **Mencarik** semua rakaman, gambar petikan, data latihan dan muat naik.
2. **Memusnahkan kunci penyulitan**, menjadikan mana-mana teks sifer yang bertahan pada medium tidak
   boleh dibaca selama-lamanya.
3. **Menggugurkan dan membina semula pangkalan data** — kamera, peraturan, amaran, akaun, segalanya.
4. **Memulakan semula** peranti ke persediaan larian pertama, seolah-olah baru dipasang.

Selepas itu anda kembali ke [bestari larian pertama](setup-wizard), log masuk dengan kata laluan
permulaan yang baharu.

## Mengapa pemadaman kripto penting {#crypto-erase}

Menulis ganti fail tidak memusnahkannya dengan boleh dipercayai pada pemacu SSD dan NVMe. Perataan
haus bermakna pengawal mungkin menyimpan salinan pada alamat fizikal yang sistem pengendalian tidak
boleh namakan pun, jadi "tulis ganti tiga kali" ialah janji yang perkakasan tidak membenarkan
perisian tunaikan.

Memusnahkan kunci mengelakkan itu sepenuhnya. Setiap rakaman telah disulitkan; kunci itu telah
hilang; apa jua bait yang tinggal ialah bunyi bising. Itulah sebabnya
[penyulitan semasa rehat](encryption-at-rest) dihidupkan secara lalai — itulah yang menjadikan
operasi ini benar-benar bermakna seperti yang dikatakannya.

## Sebelum anda melakukannya {#before}

> [!WARNING]
> Ini tidak boleh dibatalkan. Tiada sandaran melainkan anda sudah membuatnya, dan tiada kunci
> pemulihan melainkan anda sudah mengeksportnya. Setelah kunci dimusnahkan, tiada sesiapa boleh
> membaca rakaman itu lagi — bukan anda, bukan sesiapa.

Jika ada sebarang kemungkinan anda akan mahukan mana-mana daripadanya:

1. **Eksport [sandaran konfigurasi](backup-and-restore)** dan simpan di luar mesin.
2. **Salin keluar sebarang rakaman yang anda perlukan.** Klip individu boleh dimuat turun daripada
   [halaman Rakaman](recordings#exporting).
3. **Eksport [kunci pemulihan](encryption-at-rest#export)** — hanya jika anda berhasrat mengekalkan
   teks sifer itu boleh dibaca di tempat lain. Jika anda memadam untuk melupuskan mesin itu, sengaja
   *jangan* simpan ia.

## Melakukannya {#doing}

Pengesahan itu sengaja menyusahkan. Anda menaip frasa pengesahan, dan kiraan detik berjalan sebelum
ia bermula, yang boleh anda batalkan.

Setelah ia bermula, tindanan kemajuan skrin penuh muncul. **Biarkan halaman terbuka dan jangan
matikan kuasa mesin.** Pembersihan ruang bebas boleh mengambil masa pada cakera yang besar, dan
halaman akan dimuat semula dengan sendirinya sebaik peranti kembali.

Semasa set semula berjalan, peranti menolak permintaan dengan respons "tidak tersedia" yang bersih
dan bukan gagal dengan pelik — itu dijangka, bukan kerosakan.

## Apa yang bertahan {#survives}

- **`config.json`** — port pelayan, enjin pangkalan data, konfigurasi asas. Set semula memulihkan
  aplikasi kepada keadaan kilang, bukan pemasangan mesin itu.
- **Binari yang dipasang.** Versi tidak berubah.
- **Tiada apa lagi.** Bukan kamera, peraturan, amaran, akaun, rakaman, kunci atau gandingan.

Nod yang dipadam juga tidak lagi bergandingan dengan mana-mana satah kawalan, dan perlu diambil
semula.

## Bila hendak menggunakannya {#when}

- **Menyahtauliah atau melupuskan peranti.** Inilah kes utama, dan yang direka untuknya.
- **Menyerahkannya kepada pasukan atau pelanggan lain.**
- **Membina semula selepas konfigurasi yang begitu kusut sehingga bermula bersih memang lebih
  pantas.** Jarang berlaku — dan [pemulihan daripada sandaran](backup-and-restore) biasanya sampai ke
  sana dengan risiko lebih rendah.

## Apa ia bukan untuknya {#not-for}

- **Membebaskan ruang cakera.** Gunakan *Bersihkan yang tamat tempoh*, atau pendekkan pengekalan.
  Lihat [Konfigurasi rakaman](recording-configuration#purging).
- **Membetulkan pepijat.** Mulakan semula dahulu, dan semak kebergantungan runtime — lihat
  [Kemas kini, mula semula dan kesihatan](updates-and-restart#dependencies).
- **Membuang rakaman satu kamera.** *Bersihkan sekarang* pada kamera itu melakukan tepat itu.

Capai yang itu dahulu. Set semula kilang ialah alat pelupusan yang kebetulan juga membetulkan
sesuatu.
