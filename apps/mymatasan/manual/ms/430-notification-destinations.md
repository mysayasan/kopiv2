---
title: Destinasi pemberitahuan
category: notifications
categoryLabel: Pemberitahuan
summary: Hantar amaran ke webhook, Telegram atau MQTT — dengan penapisan dan muatan bagi setiap destinasi.
order: 430
---

# Destinasi pemberitahuan

Amaran sentiasa muncul dalam suapan dalam aplikasi. **Destinasi** ialah tempat lain yang turut
menerimanya, supaya seseorang mendengarnya tanpa memerhati skrin.

Konfigurasikannya dalam **Tetapan → Pemberitahuan**.

## Tiga saluran {#channels}

**Webhook** — POST HTTP ke URL yang anda berikan. Pilihan serba guna: apa sahaja yang boleh menerima
JSON boleh menggunakannya.

**Telegram** — token bot dan id sembang. Cara terpantas mendapatkan amaran ke telefon tanpa
infrastruktur anda sendiri. Gambar petikan tiba sebagai foto.

**MQTT** — URL broker dan topik, dengan id klien, QoS (0, 1 atau 2), retain, dan TLS penuh termasuk
sijil CA dan klien. Inilah yang sesuai untuk berintegrasi dengan automasi bangunan atau bas operasi
sedia ada.

## Setiap destinasi adalah bebas {#independent}

Inilah bahagian yang menjadikan ciri ini berguna, dan ia mudah terlepas pandang. Setiap destinasi
mempunyai:

- **Keterukan minimum** — satu lantai. Tetapkan laluan eskalasi kepada Kritikal sahaja dan ia kekal
  senyap sepanjang hari biasa.
- **Jenis pemberitahuan yang diterimanya** — pengesanan AI, kesihatan kamera, kesihatan mesin,
  keselamatan log masuk. Tiada yang ditanda bermakna semuanya.
- **Medan pengesanan yang disertakannya.**
- **Medan tersuai.**
- **Didayakan/dilumpuhkan**, tanpa memadamnya.

Jadi sebuah telefon mendapat Kritikal sahaja, topik MQTT operasi mendapat segalanya, dan webhook
penyelenggaraan mendapat kesihatan kamera dan mesin dan langsung tiada pengesanan. Suapan dalam
aplikasi sentiasa menunjukkan segalanya sepenuhnya tanpa mengira itu semua.

## Apa yang ada dalam muatan {#payload}

Bagi amaran AI anda memilih medan pengesanan yang hendak disertakan: peraturan, kamera, objek,
keyakinan, cap masa, zon, dan sebagainya.

**Amaran plat nombor secara automatik menyertakan** nombor plat dan, apabila dikesan, jenis dan warna
kenderaan — dalam kedua-dua teks mesej dan muatan (`plate`, `vehicleType`, `color`). Tiada suis; anda
tidak perlu ingat untuk mendayakannya.

## Gambar petikan {#snapshots}

Dua mod penghantaran:

- **Sebaris** membenamkan imej — base64 dalam muatan webhook dan MQTT, foto dalam Telegram. Penerima
  melihat gambar tanpa kerja lanjut.
- **Pautan sahaja** menghantar rujukan dan membiarkan pengguna mengambil imej itu. Muatan jauh lebih
  kecil.

Gunakan sebaris untuk manusia (mesej dengan gambar bernilai sepuluh tanpa gambar), dan pautan sahaja
untuk mesin dan untuk broker MQTT di mana muatan retain yang besar menjadi masalah.

## Medan tersuai dan pemegang tempat {#custom-fields}

Medan kunci/nilai tersuai ditambah kepada muatan, dan nilai boleh mengandungi pemegang tempat yang
diselesaikan pada masa hantar — nama kamera, nama peraturan, cap masa dan sebagainya. Penyunting
menyenaraikan token yang tersedia dan menyalinnya apabila diklik.

Inilah cara anda menjadikan muatan sesuai dengan sistem yang sudah wujud dan bukan menulis semula
sistem itu supaya sesuai dengan muatan: tambah `site: "depoh-utara"`, atau bentukkan medan menjadi
kunci tepat yang sudah dibaca automasi anda. Medan tersuai dengan kunci yang sama dengan medan
terbina dalam akan menggantikannya, dan penyunting menyatakannya. Token yang tidak menyelesaikan
kepada apa-apa akan ditinggalkan dan bukan dihantar kosong.

## Penghalaan bagi setiap peraturan {#routing}

Secara lalai setiap amaran peraturan pergi ke setiap destinasi. Pada sesuatu peraturan, pilih
destinasi tertentu untuk menyempitkannya — lihat
[Mencipta peraturan pengesanan](detection-rules#routing).

Antara penapis bagi setiap destinasi dan penghalaan bagi setiap peraturan, anda boleh menyatakan
kebanyakan apa yang sebenarnya diperlukan sesuatu tapak: peraturan perimeter selepas waktu kerja
mengejutkan seseorang, peraturan ruang muatan waktu siang tidak.

## Menguji {#testing}

**Hantar Ujian** menghantar pemberitahuan Sistem ke destinasi yang melanggan jenis itu.

Uji semasa anda menyediakannya, dan uji semula selepas menukar apa-apa tentang pihak penerima.
Laluan penghantaran yang senyap-senyap rosak kelihatan sama seperti malam yang tenang.

Perhatikan interaksinya: destinasi yang keterukan minimumnya melebihi pemberitahuan ujian, atau yang
tidak menerima pemberitahuan Sistem, tidak akan mendapat ujian itu. Itu bukan kegagalan — itu penapis
anda berfungsi.

## Apabila amaran tidak dihantar {#troubleshooting}

Mula-mula tentukan bahagian mana yang rosak: **adakah amaran itu ada dalam suapan dalam aplikasi?**

- **Tiada dalam suapan** — tiada apa yang dikesan. Ini masalah pengesanan, bukan penghantaran; lihat
  [Pemberitahuan](notifications#not-arriving).
- **Ada dalam suapan tetapi tidak dihantar** — maka turuti:
  1. Adakah destinasi itu **didayakan**?
  2. Adakah **keterukan minimumnya** membenarkan amaran ini?
  3. Adakah ia **menerima jenis** pemberitahuan ini?
  4. Adakah **peraturan** itu menghala kepadanya?
  5. Adakah **Hantar Ujian** tiba? Jika ya, pengangkutannya baik dan penapisan yang menghalangnya.

Penghantaran dicuba semula dengan tempoh berundur apabila destinasi sementara tidak boleh dicapai,
dan MQTT menunggu brokernya dan bukan membuang mesej semasa permulaan — jadi broker yang naik lewat
tidak kehilangan amaran pertama.

## Pengekalan pemberitahuan {#retention}

Pemberitahuan lama dibersihkan mengikut jadual yang anda tetapkan: hari simpan, selang pembersihan,
dan pilihan untuk hanya membersihkan yang telah dibaca. Sifar melumpuhkan pembersihan automatik.

Menyimpan pemberitahuan yang telah dibaca selama-lamanya jarang berguna; rakaman dan log amaran ialah
rekodnya. Perubahan selang berkuat kuasa selepas dimulakan semula.
