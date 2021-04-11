<?php
sleep( 1 );
header("Cache-Control: s-maxage=5");
header("Date: " . date("D, j M Y G:i:s T"));
echo "Hello, World\n";
