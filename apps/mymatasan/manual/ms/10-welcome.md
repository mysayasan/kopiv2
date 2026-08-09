---
title: Apa itu MyMataSan
category: getting-started
categoryLabel: Permulaan
summary: Lima idea asas yang membentuk keseluruhan produk — kamera, peraturan, amaran, rakaman dan peranan.
order: 10
---

# Apa itu MyMataSan

MyMataSan ialah **perakam video rangkaian yang bermata**. Ia bersambung kepada kamera yang anda
sedia ada, memerhati apa yang dilihatnya, merakam rakaman itu, dan memberitahu anda apabila sesuatu
yang anda ambil berat benar-benar berlaku.

Ia berjalan sepenuhnya pada satu mesin dalam rangkaian anda sendiri. Ia tidak memerlukan internet
untuk berfungsi: pengesanan AI, rakaman, carian dan juga manual ini semuanya berada di dalam
peranti. Jika tapak anda langsung tiada sambungan keluar, tiada apa di sini yang berhenti berfungsi.

## Lima idea asas {#concepts}

Hampir setiap skrin dalam produk ini ialah salah satu daripada lima perkara ini, jadi berbaloi
mempelajari istilahnya sekali sahaja.

**Kamera.** Sumber video dalam rangkaian anda, biasanya dicapai melalui ONVIF atau RTSP. Menambah
kamera kebanyakannya soal mencarinya dan memberi MyMataSan nama pengguna serta kata laluan
untuknya.

**Peraturan.** Arahan tetap dalam bentuk *"pada kamera ini, dalam kawasan ini, beritahu saya apabila
kamu nampak ini"*. Sesuatu peraturan menamakan satu atau lebih kelas objek (orang, kenderaan, api,
plat nombor, atau sesuatu yang anda ajar sendiri), boleh dihadkan kepada zon yang dilukis pada
bingkai, dan boleh dihadkan kepada waktu tertentu sahaja.

**Amaran.** Apa yang dihasilkan oleh peraturan apabila ia dicetuskan. Amaran membawa cap masa,
kamera terlibat, apa yang dilihat, gambar petikan dengan kotak dan label pada pengesanan itu, dan —
apabila rakaman dihidupkan — pautan kepada rakaman sekitar detik itu. Amaran terkumpul dalam suapan
**Pemberitahuan**, dan boleh juga dihantar keluar ke webhook, Telegram, broker MQTT, atau e-mel.

**Rakaman.** Rakaman berterusan yang ditulis kamera ke cakera, disimpan untuk tempoh pengekalan
pilihan anda dan kemudian dibersihkan secara automatik untuk memberi ruang. Rakaman inilah yang
menukar amaran daripada "saya dapat mesej" kepada "saya boleh buktikan apa yang berlaku".

**Peranan.** Apa yang dibenarkan untuk orang yang telah log masuk. Terdapat tiga, dan perbezaan
antaranya adalah disengajakan, bukan hiasan — lihat [Peranan dan apa yang boleh dilakukan](#roles).

## Peranan dan apa yang boleh dilakukan {#roles}

| Peranan | Boleh | Tidak boleh |
|---|---|---|
| **Pemerhati** | Menonton paparan langsung. Melihat bahawa amaran telah dicetuskan. Menukar kata laluan sendiri. | Membuka rakaman. Mengakui amaran, menggerakkan kamera, atau bercakap melaluinya. Apa-apa dalam Tetapan. |
| **Operator** | Segala yang boleh dilakukan pemerhati, tambah main semula dan muat turun rakaman, mencari apa yang dilihat kamera, mengakui amaran, pan/tilt/zum, dan bercakap melalui pembesar suara kamera. | **Memadam apa-apa** — rakaman, amaran, kamera. Menukar peraturan, tetapan atau kamera. Menguruskan pengguna. |
| **Pentadbir** | Segala-galanya. | — |

Garis yang perlu difahami ialah yang di bawah **operator**: seorang operator boleh menyemak semula
sesuatu insiden tetapi tidak boleh memusnahkan buktinya. Itulah yang menjadikan perakam ini boleh
dipercayai sebagai rekod dan bukan sekadar kemudahan, dan ia dikuatkuasakan oleh pelayan pada setiap
permintaan, bukan dengan menyembunyikan butang.

Akaun baharu dicipta sebagai operator melainkan pentadbir memilih sebaliknya.

## Apa sebenarnya yang dilakukan AI {#detection}

MyMataSan menjalankan model pengesanan ke atas bingkai daripada kamera anda dan melaporkan objek
yang dikenalinya, setiap satu dengan skor keyakinan. Sesuatu peraturan menukar pengesanan mentah itu
kepada sesuatu yang berbaloi diberi perhatian dengan menambah konteks yang tiada pada model itu:
kamera *yang mana*, *di mana* dalam bingkai, *bila*, dan *sejauh mana yakin* ia perlu sebelum anda
diberitahu.

Pembahagian itu penting apabila anda menala sesuatu kemudian. Jika sistem terlepas kejadian, model
atau ambang keyakinan biasanya puncanya. Jika sistem menemui kejadian dengan betul tetapi
memberitahu anda tentang yang salah, peraturan itulah puncanya.

## Ke mana selepas ini {#next}

- Menyediakannya buat kali pertama: [Log masuk buat kali pertama](first-sign-in).
- Sudah log masuk dan sedang meninjau: [Lawatan ruang kerja](workspace-tour).
- Memindahkan pemasangan sedia ada ke mesin ini: [Memulihkan daripada sandaran](restore-from-backup).
