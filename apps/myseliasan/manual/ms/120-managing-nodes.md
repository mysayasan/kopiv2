---
title: Mengurus nod
category: fleet
categoryLabel: Armada
summary: Namakan semula, buka skrinnya sendiri, kekalkan sijilnya, lepaskan, atau padamkannya.
order: 120
---

# Mengurus nod

Sebaik nod diambil, ia muncul dalam **Nodes** bersama statusnya, bila kali terakhir ia dilihat, dan
bila sijilnya luput. Segala yang boleh anda lakukan padanya bermula di situ.

## Butiran ialah label, bukan tetapan {#details}

**Manage → Details** menetapkan nama, keterangan dan ikon nav bagi nod itu.

Semua ini ialah **label satah kawalan — ia tidak mengubah apa-apa pada nod itu sendiri**. Menamakan
semula nod di sini tidak menamakan semula peranti itu; ia menamakan semula baris yang dibaca oleh
anda dan rakan sekerja anda. Itulah tujuannya: berikan nama yang orang sebut dengan mulut, dan
letakkan lokasi dalam keterangan, kerana armada "nvr-01, nvr-02" ialah armada yang tiada siapa boleh
fikirkan pada pukul tiga pagi.

## Bekerja pada nod itu sendiri {#node-pages}

**Manage** turut membuka skrin nod itu sendiri — kameranya, papan pemukanya, rakamannya — dari dalam
satah kawalan ini.

Halaman itu mencapai nod melalui saluran kawalan armada. Pelayar anda tidak pernah menyambung terus
kepada nod, dan itulah yang menjadikan nod di tapak jauh di belakang NAT boleh diurus dari sini.

Nod mesti **dalam talian** untuk semua ini berfungsi. Penemuan, imbasan kamera dan perubahan tetapan
semuanya berjalan pada nod, bukan di sini, jadi nod luar talian boleh dinamakan semula tetapi tidak
boleh dikonfigurasikan.

## Sijil, dan cara senyap sebuah nod meninggalkan armada {#certificate}

Setiap nod memegang sijil yang dikeluarkan oleh pihak berkuasa armada, dan sijil itu luput.

Nod memperbaharui sijilnya sendiri sebelum luput — tanpa tindakan anda — **tetapi satah kawalan
hanya membenarkan pembaharuan itu apabila auto-renew dihidupkan untuk nod tersebut**. Biarkan ia
dimatikan dan sijil itu akan luput begitu sahaja. Tiada apa yang rosak dengan bising: nod itu cuma
hilang sambungan armadanya pada suatu hari dan menjadi luar talian.

Senarai Nodes memaparkan **Cert expires** atas sebab inilah, dan ringkasan menimbulkan penemuan
apabila tarikh luput menghampiri. Jika anda hanya membaca satu lajur pada halaman itu, bacalah lajur
ini.

## Melepaskan {#releasing}

**Release** mengeluarkan nod daripada satah kawalan ini dan membolehkannya diambil semula.

Ini ialah laluan yang kemas dan ia meninggalkan kedua-dua belah pihak konsisten. Jika satah kawalan
ini pula sudah tiada atau tidak dapat dihubungi, nod boleh menggugurkan dirinya sendiri daripada
halaman Connectivity-nya — selepas itu anda perlu membersihkan baris lapuk di sini.

## Nod yang tidak dikenali {#unrecognized}

Kadangkala sebuah nod terus cuba menyambung dengan **sijil yang sah** sedangkan tiada rekodnya di
sini.

Biasanya itu nod yang dilepaskan di sebelah sini tetapi tidak pernah diset semula di sebelahnya
sendiri, atau nod yang rekodnya hilang. Ia muncul dalam **Unrecognized nodes** dan bukan diabaikan
senyap-senyap, kerana sijil yang masih berfungsi dengan pemilik yang tidak diketahui ialah gabungan
yang memang wajar dilihat.

**Block** membatalkan sijil itu, jadi ia tidak lagi boleh menyambung atau mendaftar semula. Untuk
menghentikannya daripada mencuba langsung, set semula kilang nod itu juga.

## Memadam nod dari jauh {#wipe}

**Wipe** menetapkan semula nod kepada tetapan kilang: ia memadam rakaman, kamera, peraturan AI,
pengguna dan tetapan, kemudian memulakan semula. Ia tidak boleh dibatalkan.

Ia berjalan di sebalik kiraan detik yang boleh anda batalkan, dan ia hanya berfungsi apabila nod
dalam talian **dan** membenarkan set semula jauh (`bootstrap.allowReset` pada nod). Nod yang enggan
bukanlah rosak — ia dikonfigurasikan supaya tidak membenarkan satah kawalan jauh memadamnya, dan itu
pendirian yang munasabah bagi sebuah peranti.

Release ialah apa yang anda mahu apabila memindahkan nod ke tempat baharu. Wipe pula untuk
menyahtauliah atau menyerahkan perkakasan kepada orang lain.

## Apabila nod menunjukkan luar talian {#offline}

Mengikut susunan kebarangkalian:

1. **Nod itu memang dimatikan**, atau kehilangan rangkaiannya.
2. **Sijilnya luput** kerana auto-renew dimatikan. Semak lajur Cert expires dahulu — inilah yang
   selalu mengejutkan orang.
3. **Nod tidak dapat mencapai satah kawalan ini.** Nod mendail keluar, jadi semak sebelahnya: alamat
   satah kawalan yang berubah, proksi, atau tembok api yang kini menyekat sambungan keluar.
4. **Nod telah dilepaskan atau diset semula di sebelahnya sendiri** dan tidak lagi tergolong dalam
   armada ini.

Nod luar talian mengekalkan sejarahnya di sini. Tiada apa yang telah anda kumpulkan hilang semasa ia
tiada.
