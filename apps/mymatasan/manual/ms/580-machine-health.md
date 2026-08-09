---
title: Memerhati mesin itu sendiri
category: administration
categoryLabel: Pentadbiran
summary: Pemantauan CPU, memori dan cakera, fungsi perlindungannya, dan apa yang perlu dibuat apabila ia mengadu.
order: 580
---

# Memerhati mesin itu sendiri

Kamera dipantau; begitu juga hos yang menjalankannya. **Tetapan → Kesihatan Mesin** menetapkan
ambang, dan pelanggaran tiba sebagai pemberitahuan **Kesihatan Mesin** dalam suapan.

## Apa yang diperhatikan {#watched}

- **CPU** — beban berterusan. Peranti terikat CPU dalam operasi biasa, jadi ini perkara pertama yang
  bergerak apabila anda menambah kamera atau model yang lebih besar.
- **Memori** — ruang lega. Model pengesanan kekal dalam memori; beberapa model aktif menggandakannya.
- **Cakera** — ruang bebas pada volum **rakaman** secara khusus, bukan setiap lekapan. Itulah yang
  mempunyai akibat keras.

## Mengapa amaran cakera berbeza {#disk}

Tekanan CPU dan memori merosotkan keadaan. Cakera yang penuh **menghentikan rakaman**.

Sama ada ia berhenti atau menulis ganti ialah pilihan anda — lihat
[Apabila cakera penuh](recording-configuration#disk-full) — tetapi apa jua pun, amaran cakera ialah
yang perlu ditindaki pada hari yang sama. Mitigasi disempadankan kepada volum rakaman, dan itulah
tepat sebabnya laluan storan tidak sepatutnya berada pada pemacu sistem: pemacu sistem yang penuh
menjatuhkan keseluruhan peranti, bukan hanya rakaman.

## Membaca amaran CPU berterusan {#cpu}

Hampir tidak pernah "beli mesin yang lebih besar". Turuti
[apa yang sebenarnya memakan kapasiti](storage-and-capacity#drivers) dahulu — dalam praktik:

1. Adakah **pengesanan** dihalakan ke strim utama dan bukan sub-strim? Inilah jawapan biasa.
2. Berapa banyak **model** yang aktif? Setiap satu membuat inferens pada setiap bingkai.
3. Adakah **model stok** lebih besar daripada yang dimahukan perkakasan ini?
4. Adakah jubin paparan langsung berundur kepada **MJPEG** dan bukan WebRTC?
5. Adakah **pengecaman wajah** dihidupkan pada kamera yang tidak memerlukannya?

Empat daripada lima itu ialah tetapan. Membetulkannya biasanya memulihkan lebih banyak ruang lega
berbanding mana-mana perkakasan yang boleh anda beli pada petang yang sama.

## Menetapkan ambang {#thresholds}

Tetapkannya supaya pemberitahuan bermaksud *lakukan sesuatu*.

Ambang yang dicetuskan setiap petang semasa waktu sibuk melatih semua orang mengabaikan pemberitahuan
Kesihatan Mesin — dan kemudian yang cakera, yang penting, turut diabaikan. Jika sesuatu nilai kerap
dilanggar dan peranti masih mampu, ambang itulah yang salah, bukan mesin.

## Kapasiti, sebelum dan selepas {#capacity}

Tab yang sama membawa anggaran kapasiti kamera dan **Jalankan penentukuran**. Tentukur:

- sebelum menambah kamera, supaya nombor itu mencerminkan perkakasan ini;
- selepas menukar model stok atau mengaktifkan model lain, kerana pengiraannya telah berubah;
- selepas perubahan perkakasan.

Lihat [Storan dan kapasiti](storage-and-capacity#estimate).

## Apa yang ia tidak perhatikan {#limits}

- **Pemprosesan rangkaian.** Pautan yang tepu muncul sebagai kamera tidak stabil dan jubin
  tersekat-sekat, bukan sebagai amaran kesihatan mesin. Lihat
  [kamera yang tidak stabil](camera-health#flapping).
- **Suhu dan kipas.** Gunakan pemantauan sistem pengendalian hos sendiri; peranti kecil mengurangkan
  kelajuan secara senyap apabila panas, dan pengurangan itu kelihatan sama seperti "ambang CPU asyik
  dicetuskan".
- **Volum lain.** Hanya volum rakaman yang dimitigasi. Jika pemacu sistem penuh atas sebab lain,
  pemantauan platform anda yang perlu menangkapnya.

## Di mana ini sesuai dengan yang lain {#related}

Tiga pemantau, tiga soalan, dan berbaloi mengekalkannya jelas:

| Pemantau | Menjawab |
|---|---|
| [Kesihatan kamera](camera-health) | Bolehkah peranti mencapai kamera? |
| **Kesihatan mesin** | Bolehkah mesin menampungnya? |
| [Liveness dan readiness](updates-and-restart#health) | Adakah perkhidmatan hidup dan mampu berkhidmat? |

Tapak yang memerhati ketiga-tiganya hampir tiada cara untuk rosak secara senyap.
