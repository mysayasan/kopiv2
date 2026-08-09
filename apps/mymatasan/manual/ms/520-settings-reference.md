---
title: Tetapan, tab demi tab
category: administration
categoryLabel: Pentadbiran
summary: Tujuan setiap satu daripada sembilan tab Tetapan, dan artikel mana yang meliputinya secara mendalam.
order: 520
---

# Tetapan, tab demi tab

Tetapan hanya untuk pentadbir. Sembilan tab, setiap satu dengan penerangan sebaris dalam aplikasi;
halaman ini menyatakan *tujuan* setiap satu dan di mana butirannya berada.

## Runtime {#runtime}

*Penalaan penyahkod, penstriman dan enjin pengesanan.*

Pemipaan video: laluan ffmpeg, pengangkutan RTSP (TCP ialah lalai yang boleh dipercayai), pecutan
perkakasan, saiz duga dan penimbalan, serta konfigurasi penstriman WebRTC/MJPEG.

Datang ke sini apabila paparan langsung tersekat-sekat, apabila sepanduk amaran kodek muncul, atau
selepas memasang ffmpeg secara manual. Kebanyakan tapak menetapkan ini sekali semasa persediaan dan
tidak pernah kembali.

Pecutan perkakasan ialah satu-satunya tombol yang berbaloi dicuba pada mesin yang sibuk — dan berbaloi
diundurkan dengan cepat jika strim mula gagal, kerana sokongan pecutan sangat berbeza antara pemacu.

## AI {#ai}

*Model pengesanan, ambang dan sumber bingkai.*

Pemasang runtime AI, pilihan model stok, import dan pengaktifan model tersuai, dan model plat nombor.

Diliputi oleh [Bagaimana pengesanan berfungsi](how-detection-works) dan
[Melatih model tersuai](training-models). Tetapan paling berkesan pada tab ini ialah saiz model stok —
lihat [jadual model](how-detection-works#models).

## Pemberitahuan {#notifications}

*Destinasi penghantaran, kategori dan medan amaran.*

Destinasi, penapisan bagi setiap destinasi, medan muatan, gambar petikan, dan pengekalan
pemberitahuan. Diliputi sepenuhnya oleh [Destinasi pemberitahuan](notification-destinations).

## Kesihatan Kamera {#camera-health}

*Pemantauan kebolehcapaian kamera dan amaran luar talian.*

Berapa kerap kamera diduga, had masa dugaan, dan berapa banyak kegagalan berturut-turut dikira luar
talian (dan berapa banyak kejayaan dikira pulih). Tab memberitahu anda jumlah nombor itu dalam saat,
iaitu nombor yang anda sebenarnya pedulikan.

Ambang yang lebih panjang bermakna kurang bunyi pada rangkaian tidak stabil dan berita lewat tentang
kegagalan sebenar. Lihat [Kesihatan kamera](camera-health).

## Kesihatan Mesin {#machine-health}

*Pemantauan dan perlindungan CPU, memori dan cakera hos.*

Ambang bagi sumber hos itu sendiri, berserta anggaran kapasiti kamera dan penanda aras **Jalankan
penentukuran**. Lihat [Storan dan kapasiti](storage-and-capacity).

## Pengguna {#users}

*Akaun tempatan, peranan dan pengurusan kata laluan.*

Akaun, peranan dan matriks kebenaran — lihat [Pengguna dan peranan](users-and-roles).

## Ketersambungan {#connectivity}

*Gandingan armada, penemuan dan ketersambungan nod.*

Hanya relevan apabila peranti ini ialah nod bagi armada myseliasan. Lihat
[Menyambung ke satah kawalan](control-plane).

## Sandaran & Pemulihan {#backup}

*Sandar dan pulihkan konfigurasi anda, eksport/sahkan kunci pemulihan, dan padam selamat serta set
semula.*

Tiga perkara berasingan berkongsi tab ini, dan mengelirukannya adalah mahal:

- **Sandaran konfigurasi** — tetapan anda, mudah alih ke mesin lain.
  [Menyandarkan konfigurasi anda](backup-and-restore).
- **Kunci pemulihan** — eskrow kunci penyulitan semasa rehat. Tanpanya, rakaman mesin yang mati tidak
  boleh dibaca selama-lamanya. [Penyulitan semasa rehat](encryption-at-rest).
- **Padam selamat dan set semula kilang** — musnahkan segalanya, secara sengaja.
  [Padam selamat dan set semula kilang](secure-wipe-and-reset).

## Versi & Kesihatan {#version}

*Versi aplikasi, kebergantungan runtime dan semakan kesihatan.*

Versi yang berjalan dan kawalan kemas kini, keadaan kebergantungan runtime (ffmpeg, runtime AI, OCR),
kawalan mula semula, dan liveness/readiness. Lihat
[Kemas kini, mula semula dan kesihatan](updates-and-restart).

## Perkara yang perlukan mula semula {#restart}

Kebanyakan tetapan berkuat kuasa serta-merta. Beberapa tidak, dan aplikasi melabelkannya *(mula semula
untuk berkuat kuasa)* di tempat yang benar — ffmpeg yang baru dipasang ialah yang biasa, dan selang
pembersihan pemberitahuan ialah satu lagi.

Jika sesuatu perubahan nampaknya tidak berkesan, cari label itu sebelum menganggapnya gagal.
