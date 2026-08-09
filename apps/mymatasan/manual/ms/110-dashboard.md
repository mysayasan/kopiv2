---
title: Papan pemuka
category: daily-use
categoryLabel: Penggunaan harian
summary: Membaca skrin ringkasan — maksud setiap nombor, dan panel mana yang wajar ditindaki.
order: 110
---

# Papan pemuka

Papan pemuka menjawab satu soalan: *adakah sesuatu yang luar biasa sedang berlaku?* Segala yang ada
padanya ialah ringkasan sepanjang julat masa pilihan anda — **Hari ini**, **7 hari** atau **30
hari** — daripada pemilih di bahagian atas.

## Empat pembilang {#counters}

Di bahagian atas: **Jumlah peristiwa**, **Belum dibaca**, **Kritikal** dan **Amaran**, setiap satu
dengan perbandingan terhadap tempoh sebelumnya yang sama panjang.

Perbandingan itulah bahagian yang berguna. "412 peristiwa" tidak bermakna apa-apa dengan sendirinya;
"412 peristiwa, naik 180% berbanding minggu lepas" bermakna sesuatu telah berubah. Lihat arahnya
sebelum anda melihat nomborannya.

**Belum dibaca** ialah baris gilir kerja, bukan kerosakan. Ia mengira peristiwa yang belum dilihat
sesiapa. Nombor yang meningkat setiap hari biasanya bermakna sesuatu peraturan dicetuskan lebih
kerap daripada yang mampu disemak — itu masalah penalaan, bukan masalah bilangan pekerja.

## Peristiwa mengikut masa {#events-over-time}

Jumlah pemberitahuan sepanjang julat itu, dipecahkan mengikut kategori. Gunakannya untuk mengesan
*bila* sesuatu berubah. Anak tangga yang bermula pada jam tertentu pada hari tertentu hampir selalu
sepadan dengan sesuatu yang fizikal: lampu yang menyala, pintu pagar yang dibiarkan terbuka, kamera
yang tersenggol.

## Mengikut kategori, keterukan dan sumber {#breakdowns}

Tiga pecahan bagi peristiwa yang sama. **Kategori** memisahkan pengesanan AI daripada kesihatan
kamera, kesihatan mesin dan keselamatan log masuk — wajar disemak apabila jumlahnya melonjak, kerana
lonjakan yang semuanya peristiwa kesihatan kamera ialah masalah rangkaian, bukan peristiwa
keselamatan.

## Kamera dan objek teratas {#tops}

**Kamera teratas** disusun mengikut bilangan peristiwa. **Objek dikesan teratas** menunjukkan apa
yang benar-benar dilihat AI.

Kamera di puncak senarai ini belum tentu yang menarik — ia yang paling sibuk. Kamera yang menghala
ke laluan pejalan kaki awam akan sentiasa menang. Bandingkannya dengan
[Kamera paling bising](#noise) sebelum anda membuat sebarang kesimpulan.

## Peta haba aktiviti {#heatmap}

Minggu tipikal — hari dalam minggu berbanding jam, dipuratakan sepanjang empat minggu terakhir —
dan boleh ditapis kepada satu kamera.

Inilah panel yang memberitahu anda rupa *normal* di tapak anda, dan itulah yang menjadikan
pengecualian dapat dikenali. Setelah anda tahu ruang muatan sibuk 06:00–09:00 dan sunyi selepas
19:00, kelompok pada tengah malam Selasa berhenti menjadi nombor dan mula menjadi persoalan.

## Kebolehpercayaan kamera {#reliability}

Masa beroperasi, jumlah masa luar talian dan bilangan gangguan bagi setiap kamera sepanjang tujuh
hari terakhir, berserta status semasa setiap kamera.

Baca **bilangan gangguan** sebelum peratusan masa beroperasi. Kamera pada 99% dengan satu gangguan
panjang mengalami satu insiden; 99% yang sama tersebar merentasi empat puluh gangguan pendek ialah
kamera dengan kabel yang gagal atau pautan rangkaian yang tepu, dan ia akan terus menggugurkan
bingkai — dan dengan itu pengesanan — antara gangguan yang dapat dilihat laporan itu.

## Kamera paling bising {#noise}

Kamera disusun mengikut bilangan amaran AI, berserta peratusan yang tiada sesiapa membacanya.

Peratusan belum dibaca yang tinggi ialah isyarat di sini. Ia bermakna peraturan pada kamera itu
menghasilkan amaran yang orang telah belajar untuk abaikan, dan itulah mod kegagalan yang paling
penting: operator yang mengabaikan amaran satu kamera sedang dilatih untuk mengabaikan semuanya.
Betulkannya dengan menyempitkan peraturan — zon yang dilukis, ambang keyakinan yang lebih tinggi,
atau jadual — bukan dengan meminta orang berusaha lebih.

## Pengesanan anomali {#anomaly}

Satu-satunya panel pada skrin ini yang merupakan kawalan dan bukan sekadar paparan.

Ia mempelajari aktiviti sejam-sejam yang normal bagi setiap kamera sepanjang minggu-minggu terkini
dan menimbulkan amaran apabila sesuatu kamera luar biasa **sibuk** (lonjakan) atau luar biasa
**sunyi**. Kes sunyi ialah yang sering dipandang ringan: kamera yang tiba-tiba tidak melihat apa-apa
pada jam yang ia sentiasa melihat sesuatu selalunya telah ditutup, dipusing, atau dicabut.

- **Pintar (garis dasar)** membandingkan dengan apa yang benar-benar dipelajari kamera itu. Inilah
  mod yang patut digunakan.
- **Had manual** menggunakan nombor peristiwa sejam tetap yang anda tetapkan. Gunakannya hanya di
  tempat anda tahu nombor yang betul lebih baik daripada garis dasar.
- **Kepekaan** — Tinggi, Sederhana atau Rendah — mengawal sejauh mana daripada normal barulah
  dikira luar biasa.
- **Imbas jam terakhir** memaparkan pratonton apa yang akan memberi amaran, tanpa mengubah apa-apa.
  Gunakannya selepas setiap perubahan kepada kepekaan, dan bukan menunggu untuk mengetahuinya pada
  waktu malam.

## Apabila papan pemuka kosong {#empty}

Pemasangan baharu tidak menunjukkan apa-apa sehingga peristiwa wujud. Jika ia masih kosong selepas
kamera berjalan agak lama, punca biasanya ialah belum ada peraturan pengesanan — rakaman semata-mata
tidak menghasilkan peristiwa. Lihat [Mencipta peraturan pengesanan](detection-rules).
