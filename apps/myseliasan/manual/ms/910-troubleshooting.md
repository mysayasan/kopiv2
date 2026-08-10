---
title: Menyelesaikan masalah
category: reference
categoryLabel: Rujukan
summary: Perkara yang benar-benar tidak menjadi, mengikut susunan ia benar-benar berlaku.
order: 910
---

# Menyelesaikan masalah

Setiap entri bermula dengan punca yang paling berkemungkinan, bukan yang paling menarik.

## Penemuan tidak menjumpai sebarang nod {#discovery}

Hampir selalu **kunci armada**: penemuan memang senyap tanpa kunci yang sepadan, dan nod tanpa kunci
atau dengan kunci berbeza langsung tidak menjawab.

Kemudian, mengikut susunan: nod itu **sudah diambil** (nod yang diambil menjadi senyap), ia berada
pada **subnet lain** (multicast tidak dihalakan), **kod tuntutan luput**, atau tembok api menyekat
trafik itu. Pengambilan **melalui alamat** sentiasa berfungsi apabila penemuan tidak nampak nod itu
— lihat [Mengambil nod](adopting-nodes#troubleshooting).

## Nod menunjukkan luar talian {#node-offline}

Semak **sijil** sebelum apa-apa lagi. Nod memperbaharui sijilnya sendiri, tetapi hanya selagi
auto-renew dihidupkan untuknya; jika dibiarkan mati, sijil itu luput dan nod tercicir daripada
armada tanpa apa-apa rosak dengan bising. Senarai Nodes memaparkan *Cert expires* atas sebab ini.

Selain itu: nod itu memang dimatikan, ia **tidak dapat mencapai satah kawalan ini** (ia mendail
keluar, jadi semak sebelahnya — alamat, proksi, tembok api), atau ia telah dilepaskan atau diset
semula dari hujungnya sendiri. Lihat [Mengurus nod](managing-nodes#offline).

## Nod terus menyambung tetapi tiada dalam senarai {#unrecognized}

Ia muncul di bawah **Unrecognized nodes**: sijil yang sah tanpa rekod di sini, biasanya nod yang
dilepaskan di sebelah ini tetapi tidak pernah diset semula di sebelahnya sendiri.

**Block** membatalkan sijilnya. Untuk menghentikannya daripada mencuba langsung, set semula kilang
nod itu juga.

## Peraturan armada tidak pernah menyala {#rule-silent}

**Teks tidak padan** jauh lebih kerap daripada apa-apa sebab lain — ia dipadankan dengan nama
peraturan nod itu sendiri, jadi semak perkataan tepat dalam [suapan](notifications) dan bukan menaip
apa yang anda sangka tertulis.

Kemudian: **tetingkap terlalu pendek**, ada **ketiadaan yang melucutkannya** (lencana yang tiba
dalam tempoh ihsan ialah kelakuan yang betul — kemasukan itu dibenarkan), **jenis nod salah**, atau
peraturan itu dimatikan atau masih dalam tempoh sejuk. Lihat
[Peraturan armada](fleet-rules#troubleshooting).

## Terlalu banyak amaran {#noise}

Tambah **ketiadaan** sebelum anda menaikkan ambang: "gerakan di ruang muatan" menyala sepanjang
hari; "gerakan tanpa leretan lencana dan tanpa penghantaran berjadual" menyala apabila ada sesuatu
yang tidak kena.

Menaikkan tempoh sejuk memampatkan ribut menjadi satu amaran tetapi tidak menjadikan peraturan yang
salah itu betul. Jika satu sumber mendominasi, betulkan peraturan **pada nod** — lihat
[suapan](notifications#noise).

## Rakan sekerja tidak nampak sesuatu halaman {#permissions}

Menu yang hilang bermakna kebenaran yang hilang; tiada tetapan paparan.

Mengikut susunan: mereka **belum ada peranan** (skrin menunggu), peranan mereka **tiada peraturan**
bagi laluan itu (tiada peraturan bermakna ditolak), **suis menu** dimatikan, mereka kekurangan
**akses nod** dan bukan akses satah kawalan, atau akaun itu dilumpuhkan. Lihat
[Pengguna, peranan dan akses](users-and-roles#troubleshooting).

## Peta tiada jalan {#map-blank}

Tiada **peta asas luar talian** dipasang. Penanda, bangunan dan pelan lantai tetap berfungsi — hanya
latar belakang yang tiada. Lihat [Peta armada](the-map#basemap).

Muat turun wilayah memerlukan alat `pmtiles` pada pelayan dan **menghubungi internet**, iaitu
langkah yang salah di tapak yang sepatutnya tiada trafik keluar.

## Nama kosong dalam laporan PDF {#report-blank}

Teks laporan menggunakan fon tulisan Latin, jadi **aksara CJK dan Arab dalam nama akan kosong**.
Berikan bangunan, kawasan atau nod yang terlibat nama bertulisan Latin, atau kekalkan nama Latin di
samping nama tempatan. Lihat [Laporan](reports#latin-only).

## Pembantu tidak mahu menjawab {#assistant}

- *Tiada model bahasa diaktifkan* — keadaan yang dijangka bagi pemasangan baharu. Ringkasan tetap
  berfungsi, dan anda masih boleh **mencari manual**. Lihat [Tanya armada](ask-the-fleet#no-model).
- *Masih memuatkan* — model mengambil masa untuk dimuatkan; cuba lagi sebentar.
- *Gagal* — semak keadaan sidecar dan gunakan **Restart sidecar**. Lihat
  [Menyediakan model bahasa](language-model#degradation).
- *Menjawab dalam bahasa yang salah* — model kecil yang kembali kepada bahasa Inggeris ialah batasan
  model, bukan tetapan.

## Sesuatu tetapan tidak berkuat kuasa {#settings}

Tetapan ditulis ke `config.json` dan berkuat kuasa **selepas mula semula**. Menyimpan sahaja tidak
mengubah apa-apa yang sedang berjalan.

Jika aplikasi tidak mahu bermula selepas sesuatu perubahan, betulkan nilai itu dalam `config.json`
pada cakera — lihat [Penyunting tetapan](settings#recovery).

## Sesuatu peristiwa tiada klip {#no-clip}

Suapan menyimpan baris peristiwa; **rakaman kekal pada nod** yang merakamnya. Klip yang tiada
bermakna nod itu tidak merakam kamera tersebut, atau telah memutar keluar rakaman itu. Lihat
[suapan](notifications#evidence).

## Apabila anda perlu tahu apa yang berlaku {#audit}

Baca [log audit](audit-log#using). Nod yang hilang sama ada dilepaskan oleh seseorang, menggugurkan
dirinya dari sebelahnya sendiri, atau sekadar luar talian tanpa apa-apa dilog — tiga jawapan berbeza
yang kelihatan serupa daripada senarai Nodes.
